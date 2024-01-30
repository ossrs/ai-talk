package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
	"github.com/sashabaranov/go-openai"
	"io"
	"math/big"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var talkServer *TalkServer
var workDir string
var robots []*Robot
var asrService ASRService
var ttsService TTSService

type ASRResult struct {
	Text     string
	Duration time.Duration
}

type ASRService interface {
	RequestASR(ctx context.Context, filepath, language, prompt string, onBeforeRequest func()) (*ASRResult, error)
}

type TTSService interface {
	RequestTTS(ctx context.Context, buildFilepath func(ext string) string, text string) error
}

// The Robot is a robot that user can talk with.
type Robot struct {
	// The robot uuid.
	uuid string
	// The robot label.
	label string
	// The robot prompt.
	prompt string
	// The robot ASR language.
	asrLanguage string
	// The prefix for TTS for the first sentence if too short.
	prefix string
	// The welcome voice url.
	voice string
	// Reply words limit.
	replyLimit int
	// AI Chat model.
	chatModel string
	// AI Chat message window.
	chatWindow int
}

// Get the robot by uuid.
func GetRobot(uuid string) *Robot {
	for _, robot := range robots {
		if robot.uuid == uuid {
			return robot
		}
	}
	return nil
}

func (v Robot) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("uuid:%v,label:%v,asr:%v", v.uuid, v.label, v.asrLanguage))
	if v.prefix != "" {
		sb.WriteString(fmt.Sprintf(",prefix:%v", v.prefix))
	}
	sb.WriteString(fmt.Sprintf(",voice=%v,limit=%v,model=%v,window=%v,prompt:%v",
		v.voice, v.replyLimit, v.chatModel, v.chatWindow, v.prompt))
	return sb.String()
}

// The Stage is a stage of conversation, when user click start with a scenario,
// we will create a stage object.
type Stage struct {
	// Stage UUID
	sid string
	// Last update of stage.
	update time.Time
	// The TTS worker for this stage.
	ttsWorker *TTSWorker
	// The logging context, to write all logs in one context for a sage.
	loggingCtx context.Context
	// Previous ASR text, to use as prompt for next ASR.
	previousAsrText string
	// Previous chat text, to use as prompt for next chat.
	previousUser, previousAssitant string
	// The chat history, to use as prompt for next chat.
	histories []openai.ChatCompletionMessage
	// Whether the stage is generating more sentences.
	generating bool

	// For time cost statistic.
	lastSentence time.Time
	// The time for last upload audio.
	lastUploadAudio time.Time
	// The time for last extract audio for ASR.
	lastExtractAudio time.Time
	// The time for last request ASR result.
	lastRequestASR time.Time
	// The last request ASR text.
	lastRequestAsrText string
	// The ASR duration of audio file.
	lastAsrDuration time.Duration
	// The time for last request Chat result, the first segment.
	lastRequestChat time.Time
	// The last response text of robot.
	lastRobotFirstText string
	// The time for last request TTS result, the first segment.
	lastRequestTTS time.Time
	// The time for last download the TTS result, the first segment.
	lastDownloadAudio time.Time
}

func NewStage(opts ...func(*Stage)) *Stage {
	v := &Stage{
		// Create new UUID.
		sid: uuid.NewString(),
		// Update time.
		update: time.Now(),
		// The TTS worker.
		ttsWorker: NewTTSWorker(),
	}

	for _, opt := range opts {
		opt(v)
	}
	return v
}

func (v *Stage) Close() error {
	return v.ttsWorker.Close()
}

func (v *Stage) Expired() bool {
	if os.Getenv("AIT_DEVELOPMENT") == "true" {
		return time.Since(v.update) > 30*time.Second
	}

	if to, err := strconv.ParseInt(os.Getenv("AIT_STAGE_TIMEOUT"), 10, 64); err == nil {
		return time.Since(v.update) > time.Duration(to)*time.Second
	}

	return time.Since(v.update) > 300*time.Second
}

func (v *Stage) KeepAlive() {
	v.update = time.Now()
}

func (v *Stage) total() float64 {
	if v.lastDownloadAudio.After(v.lastSentence) {
		return float64(v.lastDownloadAudio.Sub(v.lastSentence)) / float64(time.Second)
	}
	return 0
}

func (v *Stage) upload() float64 {
	if v.lastUploadAudio.After(v.lastSentence) {
		return float64(v.lastUploadAudio.Sub(v.lastSentence)) / float64(time.Second)
	}
	return 0
}

func (v *Stage) exta() float64 {
	if v.lastExtractAudio.After(v.lastUploadAudio) {
		return float64(v.lastExtractAudio.Sub(v.lastUploadAudio)) / float64(time.Second)
	}
	return 0
}

func (v *Stage) asr() float64 {
	if v.lastRequestASR.After(v.lastExtractAudio) {
		return float64(v.lastRequestASR.Sub(v.lastExtractAudio)) / float64(time.Second)
	}
	return 0
}

func (v *Stage) chat() float64 {
	if v.lastRequestChat.After(v.lastRequestASR) {
		return float64(v.lastRequestChat.Sub(v.lastRequestASR)) / float64(time.Second)
	}
	return 0
}

func (v *Stage) tts() float64 {
	if v.lastRequestTTS.After(v.lastRequestChat) {
		return float64(v.lastRequestTTS.Sub(v.lastRequestChat)) / float64(time.Second)
	}
	return 0
}

func (v *Stage) download() float64 {
	if v.lastDownloadAudio.After(v.lastRequestTTS) {
		return float64(v.lastDownloadAudio.Sub(v.lastRequestTTS)) / float64(time.Second)
	}
	return 0
}

// The AnswerSegment is a segment of answer, which is a sentence.
type AnswerSegment struct {
	// Request UUID.
	rid string
	// Answer segment UUID.
	asid string
	// The text of this answer segment.
	text string
	// The TTS file path.
	ttsFile string
	// Whether TTS is done, ready to play.
	ready bool
	// Whether TTS is error, failed.
	err error
	// Whether dummy segment, to identify the request is alive.
	dummy bool
	// Signal to remove the TTS file immediately.
	removeSignal chan bool
	// Whether we have logged this segment.
	logged bool
	// Whether the segment is the first response.
	first bool
}

func NewAnswerSegment(opts ...func(segment *AnswerSegment)) *AnswerSegment {
	v := &AnswerSegment{
		// Request UUID.
		rid: uuid.NewString(),
		// Audio Segment UUID.
		asid: uuid.NewString(),
		// Signal to remove the TTS file.
		removeSignal: make(chan bool, 1),
	}

	for _, opt := range opts {
		opt(v)
	}
	return v
}

// The TalkServer is the AI talk server, manage stages.
type TalkServer struct {
	// All stages created by user.
	stages []*Stage

	// Total conversations.
	conversations uint64
	// Total errors.
	errors uint64
	// Total badcases.
	badcases uint64

	// The lock to protect fields.
	lock sync.Mutex
}

func NewTalkServer() *TalkServer {
	return &TalkServer{
		stages: []*Stage{},
	}
}

func (v *TalkServer) Close() error {
	return nil
}

func (v *TalkServer) NewBadcase() {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.badcases++
}

func (v *TalkServer) NewError() {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.errors++
}

func (v *TalkServer) NewConversation() {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.conversations++
}

func (v *TalkServer) AddStage(stage *Stage) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.stages = append(v.stages, stage)
}

func (v *TalkServer) RemoveStage(stage *Stage) {
	v.lock.Lock()
	defer v.lock.Unlock()

	for i, s := range v.stages {
		if s.sid == stage.sid {
			v.stages = append(v.stages[:i], v.stages[i+1:]...)
			return
		}
	}
}

func (v *TalkServer) CountStage() int {
	v.lock.Lock()
	defer v.lock.Unlock()

	return len(v.stages)
}

func (v *TalkServer) QueryStage(rid string) *Stage {
	v.lock.Lock()
	defer v.lock.Unlock()

	for _, s := range v.stages {
		if s.sid == rid {
			return s
		}
	}

	return nil
}

// The TTSWorker is a worker to convert answers from text to audio.
type TTSWorker struct {
	segments []*AnswerSegment
	lock     sync.Mutex
	wg       sync.WaitGroup
}

func NewTTSWorker() *TTSWorker {
	return &TTSWorker{
		segments: []*AnswerSegment{},
	}
}

func (v *TTSWorker) Close() error {
	v.wg.Wait()
	return nil
}

func (v *TTSWorker) QuerySegment(rid, asid string) *AnswerSegment {
	v.lock.Lock()
	defer v.lock.Unlock()

	for _, s := range v.segments {
		if s.rid == rid && s.asid == asid {
			return s
		}
	}

	return nil
}

func (v *TTSWorker) QueryAnyReadySegment(ctx context.Context, stage *Stage, rid string) *AnswerSegment {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}

		// When there is no segments, and AI is generating the sentence, we need to wait. For example,
		// if the first sentence is very short, maybe we got it quickly, but the second sentence is very
		// long so that the AI need more time to generate it.
		var s *AnswerSegment
		for ctx.Err() == nil && s == nil && stage.generating {
			if s = v.query(rid); s == nil {
				select {
				case <-ctx.Done():
				case <-time.After(100 * time.Millisecond):
				}
			}
		}

		// Try to fetch one again, because maybe there is new segment.
		s = v.query(rid)

		// All segments are consumed, we return nil.
		if s == nil {
			return nil
		}

		// Wait for dummy segment to be removed.
		if s.dummy {
			continue
		}

		// When segment is finished(ready or error), we return it.
		if s.ready || s.err != nil {
			return s
		}
	}

	return nil
}

func (v *TTSWorker) query(rid string) *AnswerSegment {
	v.lock.Lock()
	defer v.lock.Unlock()

	for _, s := range v.segments {
		if s.rid == rid {
			return s
		}
	}

	return nil
}

func (v *TTSWorker) RemoveSegment(asid string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	for i, s := range v.segments {
		if s.asid == asid {
			v.segments = append(v.segments[:i], v.segments[i+1:]...)
			return
		}
	}
}

func (v *TTSWorker) SubmitSegment(ctx context.Context, stage *Stage, segment *AnswerSegment) {
	// Append the sentence to queue.
	func() {
		v.lock.Lock()
		defer v.lock.Unlock()

		v.segments = append(v.segments, segment)
	}()

	// Ignore the dummy sentence.
	if segment.dummy {
		return
	}

	// Now that we have a real sentence, we should remove the dummy sentence.
	if dummy := v.query(segment.rid); dummy != nil && dummy.dummy {
		v.RemoveSegment(dummy.asid)
	}

	// Start a goroutine to do TTS task.
	v.wg.Add(1)
	go func() {
		defer v.wg.Done()

		if err := ttsService.RequestTTS(ctx, func(ext string) string {
			segment.ttsFile = path.Join(workDir,
				fmt.Sprintf("assistant-%v-sentence-%v-tts.%v", segment.rid, segment.asid, ext),
			)
			return segment.ttsFile
		}, segment.text); err != nil {
			segment.err = err
		} else {
			segment.ready = true
			if segment.first {
				stage.lastRequestTTS = time.Now()
			}
			logger.Tf(ctx, "File saved to %v, %v", segment.ttsFile, segment.text)
		}

		// Start a goroutine to remove the sentence.
		v.wg.Add(1)
		go func() {
			defer v.wg.Done()

			select {
			case <-ctx.Done():
			case <-time.After(300 * time.Second):
			case <-segment.removeSignal:
			}

			logger.Tf(ctx, "Remove %v %v", segment.asid, segment.ttsFile)

			stage.ttsWorker.RemoveSegment(segment.asid)

			if segment.ttsFile != "" && os.Getenv("AIT_KEEP_FILES") != "true" {
				if _, err := os.Stat(segment.ttsFile); err == nil {
					os.Remove(segment.ttsFile)
				}
			}
		}()
	}()
}

// When user start a scenario or stage, response a stage object, which identified by sid or stage id.
func handleStageStart(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	ctx = logger.WithContext(ctx)
	stage := NewStage(func(stage *Stage) {
		stage.loggingCtx = ctx
	})

	talkServer.AddStage(stage)
	logger.Tf(ctx, "Stage: Create new stage sid=%v, all=%v", stage.sid, talkServer.CountStage())

	go func() {
		defer stage.Close()

		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				if stage.Expired() {
					logger.Tf(ctx, "Stage: Remove %v for expired, update=%v",
						stage.sid, stage.update.Format(time.RFC3339))
					talkServer.RemoveStage(stage)
					return
				}
			}
		}
	}()

	type StageRobotResult struct {
		UUID  string `json:"uuid"`
		Label string `json:"label"`
		Voice string `json:"voice"`
	}
	type StageResult struct {
		StageID string             `json:"sid"`
		Robots  []StageRobotResult `json:"robots"`
	}
	r0 := &StageResult{
		StageID: stage.sid,
	}
	for _, robot := range robots {
		r0.Robots = append(r0.Robots, StageRobotResult{
			UUID:  robot.uuid,
			Label: robot.label,
			Voice: robot.voice,
		})
	}

	ohttp.WriteData(ctx, w, r, r0)
	return nil
}

func handleStartConversation(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	// The stage uuid, user must create it before upload question audio.
	q := r.URL.Query()
	sid := q.Get("sid")
	if sid == "" {
		return errors.Errorf("empty sid")
	}

	stage := talkServer.QueryStage(sid)
	if stage == nil {
		return errors.Errorf("invalid sid %v", sid)
	}

	// Keep alive the stage.
	stage.KeepAlive()
	stage.lastSentence = time.Now()

	talkServer.NewConversation()

	ohttp.WriteData(ctx, w, r, nil)
	return nil
}

// When user ask a question, which is a request with audio, which is identified by rid (request id).
func handleUploadQuestionAudio(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	// The stage uuid, user must create it before upload question audio.
	q := r.URL.Query()
	sid := q.Get("sid")
	if sid == "" {
		return errors.Errorf("empty sid")
	}

	stage := talkServer.QueryStage(sid)
	if stage == nil {
		return errors.Errorf("invalid sid %v", sid)
	}

	// Keep alive the stage.
	stage.KeepAlive()
	// Switch to the context of stage.
	ctx = stage.loggingCtx

	// Handle request and log with error.
	if err := func() error {
		// Get the robot to talk with.
		robotUUID := q.Get("robot")
		if robotUUID == "" {
			return errors.Errorf("empty robot")
		}

		robot := GetRobot(robotUUID)
		if robot == nil {
			return errors.Errorf("invalid robot %v", robotUUID)
		}

		// The rid is the request id, which identify this request, generally a question.
		rid := uuid.NewString()
		inputFile := path.Join(workDir, fmt.Sprintf("assistant-%v-input.audio", rid))
		logger.Tf(ctx, "Stage: Got question sid=%v, umi=%v, robot=%v(%v), rid=%v, input=%v",
			sid, q.Get("umi"), robot.uuid, robot.label, rid, inputFile)

		// We save the input audio to *.audio file, it can be aac or opus codec.
		if os.Getenv("AIT_KEEP_FILES") != "true" {
			defer os.Remove(inputFile)
		}
		if err := func() error {
			r.ParseMultipartForm(20 * 1024 * 1024)
			file, _, err := r.FormFile("file")
			if err != nil {
				return errors.Errorf("Error retrieving the file")
			}
			defer file.Close()

			out, err := os.Create(inputFile)
			if err != nil {
				return errors.Errorf("Unable to create the file for writing")
			}
			defer out.Close()

			nn, err := io.Copy(out, file)
			if err != nil {
				return errors.Errorf("Error writing the file")
			}
			logger.Tf(ctx, "File saved to %v, size: %v", inputFile, nn)

			return nil
		}(); err != nil {
			return errors.Wrapf(err, "copy %v", inputFile)
		}
		stage.lastUploadAudio = time.Now()

		// Do ASR, convert to text.
		var asrText string
		if resp, err := asrService.RequestASR(ctx, inputFile, robot.asrLanguage, stage.previousAsrText, func() {
			stage.lastExtractAudio = time.Now()
		}); err != nil {
			return errors.Wrapf(err, "transcription")
		} else {
			asrText = strings.TrimSpace(resp.Text)
			stage.previousAsrText = asrText
			stage.lastRequestASR = time.Now()
			stage.lastAsrDuration = resp.Duration
			stage.lastRequestAsrText = asrText
		}
		logger.Tf(ctx, "ASR ok, robot=%v(%v), lang=%v, speech=%v, prompt=<%v>, resp is <%v>",
			robot.uuid, robot.label, robot.asrLanguage, stage.lastAsrDuration, stage.previousAsrText, asrText)

		// Important trace log.
		logger.Tf(ctx, "You: %v", asrText)

		// Detect empty input and filter badcase.
		if err := func() error {
			if asrText == "" {
				return errors.Errorf("empty asr")
			}
			if robot.asrLanguage == "zh" {
				if strings.Contains(asrText, "请不吝点赞") ||
					strings.Contains(asrText, "支持明镜与点点栏目") ||
					strings.Contains(asrText, "谢谢观看") ||
					strings.Contains(asrText, "請不吝點贊") ||
					strings.Contains(asrText, "支持明鏡與點點欄目") {
					return errors.Errorf("badcase: %v", asrText)
				}
				if strings.Contains(asrText, "字幕由") && strings.Contains(asrText, "社群提供") {
					return errors.Errorf("badcase: %v", asrText)
				}
			} else if robot.asrLanguage == "en" {
				if strings.ToLower(asrText) == "you" ||
					strings.Count(asrText, ".") == len(asrText) {
					return errors.Errorf("badcase: %v", asrText)
				}
			}

			return nil
		}(); err != nil {
			talkServer.NewBadcase()
			return err
		}

		// Keep alive the stage.
		stage.KeepAlive()

		// Insert a dummy sentence to identify the request is alive.
		stage.ttsWorker.SubmitSegment(ctx, stage, NewAnswerSegment(func(segment *AnswerSegment) {
			segment.rid = rid
			segment.dummy = true
		}))

		// Do chat, get the response in stream.
		chatService := &openaiChatService{
			onFirstResponse: func(ctx context.Context, text string) {
				stage.lastRequestChat = time.Now()
				stage.lastRobotFirstText = text
			},
		}
		if err := chatService.RequestChat(ctx, rid, stage, robot); err != nil {
			return errors.Wrapf(err, "chat")
		}

		// Response the request UUID and pulling the response.
		ohttp.WriteData(ctx, w, r, struct {
			RequestUUID string `json:"rid"`
			ASR         string `json:"asr"`
		}{
			RequestUUID: rid,
			ASR:         asrText,
		})
		return nil
	}(); err != nil {
		talkServer.NewError()
		logger.Wf(ctx, "Stage: Upload err %v", err.Error())
		return err
	}
	return nil
}

// When user query the question state, which is identified by rid (request id).
func handleQueryQuestionState(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	// The stage uuid, user must create it before upload question audio.
	q := r.URL.Query()
	sid := q.Get("sid")
	if sid == "" {
		return errors.Errorf("empty sid")
	}

	stage := talkServer.QueryStage(sid)
	if stage == nil {
		return errors.Errorf("invalid sid %v", sid)
	}

	// Keep alive the stage.
	stage.KeepAlive()
	// Switch to the context of stage.
	ctx = stage.loggingCtx

	// Handle request and log with error.
	if err := func() error {
		// The rid is the request id, which identify this request, generally a question.
		rid := q.Get("rid")
		if rid == "" {
			return errors.Errorf("empty rid")
		}
		logger.Tf(ctx, "Stage: Query sid=%v, rid=%v", sid, rid)

		segment := stage.ttsWorker.QueryAnyReadySegment(ctx, stage, rid)
		if segment == nil {
			logger.Tf(ctx, "TTS: No segment for sid=%v, rid=%v", sid, rid)
			ohttp.WriteData(ctx, w, r, struct {
				AnswerSegmentUUID string `json:"asid"`
			}{})
			return nil
		}

		ohttp.WriteData(ctx, w, r, struct {
			// Whether is processing.
			Processing bool `json:"processing"`
			// The UUID for this answer segment.
			AnswerSegmentUUID string `json:"asid"`
			// The TTS text.
			TTS string `json:"tts"`
		}{
			// Whether is processing.
			Processing: segment.dummy || (!segment.ready && segment.err == nil),
			// The UUID for this answer segment.
			AnswerSegmentUUID: segment.asid,
			// The TTS text.
			TTS: segment.text,
		})
		return nil
	}(); err != nil {
		talkServer.NewError()
		logger.Wf(ctx, "Stage: Query err %v", err.Error())
		return err
	}
	return nil
}

// When user download the answer tts, which is identified by rid (request id) and aid (answer id)
func handleDownloadAnswerTTS(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	// The stage uuid, user must create it before upload question audio.
	q := r.URL.Query()
	sid := q.Get("sid")
	if sid == "" {
		return errors.Errorf("empty sid")
	}

	stage := talkServer.QueryStage(sid)
	if stage == nil {
		return errors.Errorf("invalid sid %v", sid)
	}

	// Keep alive the stage.
	stage.KeepAlive()
	// Switch to the context of stage.
	ctx = stage.loggingCtx

	// Handle request and log with error.
	if err := func() error {
		// The rid is the request id, which identify this request, generally a question.
		rid := q.Get("rid")
		if rid == "" {
			return errors.Errorf("empty rid")
		}

		asid := q.Get("asid")
		if asid == "" {
			return errors.Errorf("empty asid")
		}
		logger.Tf(ctx, "Stage: Download sid=%v, rid=%v, asid=%v", sid, rid, asid)

		// Get the segment and response it.
		segment := stage.ttsWorker.QuerySegment(rid, asid)
		if segment == nil {
			return errors.Errorf("no segment for %v %v", rid, asid)
		}
		logger.Tf(ctx, "Query segment rid=%v, asid=%v, dummy=%v, segment=%v, err=%v",
			rid, asid, segment.dummy, segment.text, segment.err)

		if !segment.logged && segment.first {
			stage.lastDownloadAudio = time.Now()
			speech := float64(stage.lastAsrDuration) / float64(time.Second)
			logger.Tf(ctx, "Elapsed cost total=%.1fs, steps=[upload=%.1fs,exta=%.1fs,asr=%.1fs,chat=%.1fs,tts=%.1fs,download=%.1fs], ask=%v, speech=%.1fs, answer=%v",
				stage.total(), stage.upload(), stage.exta(), stage.asr(), stage.chat(), stage.tts(), stage.download(),
				stage.lastRequestAsrText, speech, stage.lastRobotFirstText)
		}

		// Important trace log. Note that browser may request multiple times, so we only log for the first
		// request to reduce logs.
		if !segment.logged {
			segment.logged = true
			logger.Tf(ctx, "Bot: %v", segment.text)
		}

		// Read the ttsFile and response it as opus audio.
		if strings.HasSuffix(segment.ttsFile, ".wav") {
			w.Header().Set("Content-Type", "audio/wav")
		} else {
			w.Header().Set("Content-Type", "audio/aac")
		}
		http.ServeFile(w, r, segment.ttsFile)

		return nil
	}(); err != nil {
		talkServer.NewError()
		logger.Wf(ctx, "Stage: Query err %v", err.Error())
		return err
	}
	return nil
}

// When user remove the answer tts, which is identified by rid (request id) and aid (answer id)
func handleRemoveAnswerTTS(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	// The stage uuid, user must create it before upload question audio.
	q := r.URL.Query()
	sid := q.Get("sid")
	if sid == "" {
		return errors.Errorf("empty sid")
	}

	stage := talkServer.QueryStage(sid)
	if stage == nil {
		return errors.Errorf("invalid sid %v", sid)
	}

	// Keep alive the stage.
	stage.KeepAlive()
	// Switch to the context of stage.
	ctx = stage.loggingCtx

	// Handle request and log with error.
	if err := func() error {
		rid := q.Get("rid")
		if rid == "" {
			return errors.Errorf("empty rid")
		}

		asid := q.Get("asid")
		if asid == "" {
			return errors.Errorf("empty asid")
		}
		logger.Tf(ctx, "Stage: Remove sid=%v, rid=%v, asid=%v", sid, rid, asid)

		// Notify to remove the segment.
		segment := stage.ttsWorker.QuerySegment(rid, asid)
		if segment == nil {
			return errors.Errorf("no segment for %v %v", rid, asid)
		}

		// Remove it.
		stage.ttsWorker.RemoveSegment(asid)

		select {
		case <-ctx.Done():
		case segment.removeSignal <- true:
		}

		ohttp.WriteData(ctx, w, r, nil)
		return nil
	}(); err != nil {
		talkServer.NewError()
		logger.Wf(ctx, "Stage: Query err %v", err.Error())
		return err
	}
	return nil
}

// Serve static files.
func handleStaticFiles(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	filename := r.URL.Path[len("/api/ai-talk/examples/"):]
	if !strings.Contains(filename, ".") {
		filename = fmt.Sprintf("%v.aac", filename)
	}

	// If there is an optional stage id, we will use the logging context of stage.
	q := r.URL.Query()
	if sid := q.Get("sid"); sid != "" {
		if stage := talkServer.QueryStage(sid); stage != nil {
			ctx = stage.loggingCtx
		}
	}

	ext := strings.Trim(path.Ext(filename), ".")
	contentType := fmt.Sprintf("audio/%v", ext)
	logger.Tf(ctx, "Serve example file=%v, ext=%v, contentType=%v", filename, ext, contentType)

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, path.Join(workDir, filename))
	return nil
}

func doMain(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := doConfig(ctx); err != nil {
		return errors.Wrapf(err, "config")
	}

	// Create services.
	if os.Getenv("TENCENT_SPEECH_APPID") != "" {
		asrService = NewTencentASRService()
		ttsService = NewTencentTTSService()
		logger.Tf(ctx, "Use Tencent ASR and TTS.")
	} else {
		asrService = NewOpenAIASRService()
		ttsService = NewOpenAITTSService()
		logger.Tf(ctx, "Use OpenAI ASR and TTS.")
	}

	// Create the talk server.
	talkServer = NewTalkServer()
	defer talkServer.Close()

	// Signal handler.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		logger.Tf(ctx, "Got signal %v", sig)
		cancel()
	}()

	go func() {
		for {
			logger.Tf(ctx, "Timer: Current stages=%v, chats=%v, errors=%v, badcases=%v",
				talkServer.CountStage(), talkServer.conversations, talkServer.errors, talkServer.badcases)
			time.Sleep(10 * time.Second)
		}
	}()

	// HTTP API handlers.
	handler := http.NewServeMux()

	handler.HandleFunc("/api/ai-talk/start/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleStageStart(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle start failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler.HandleFunc("/api/ai-talk/conversation/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleStartConversation(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle audio failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler.HandleFunc("/api/ai-talk/upload/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleUploadQuestionAudio(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle audio failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler.HandleFunc("/api/ai-talk/query/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleQueryQuestionState(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle query failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler.HandleFunc("/api/ai-talk/tts/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleDownloadAnswerTTS(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle tts failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler.HandleFunc("/api/ai-talk/remove/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleRemoveAnswerTTS(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle remove failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// You can access:
	//		/api/ai-talk/examples/example.opus
	//		/api/ai-talk/examples/example.aac
	//		/api/ai-talk/examples/example.mp4
	handler.HandleFunc("/api/ai-talk/examples/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleStaticFiles(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle static files failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// httpCreateProxy create a reverse proxy for target URL.
	httpCreateProxy := func(targetURL string) (*httputil.ReverseProxy, error) {
		target, err := url.Parse(targetURL)
		if err != nil {
			return nil, errors.Wrapf(err, "parse backend %v", targetURL)
		}

		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ModifyResponse = func(resp *http.Response) error {
			// We will set the server field.
			resp.Header.Del("Server")

			// We will set the CORS headers.
			resp.Header.Del("Access-Control-Allow-Origin")
			resp.Header.Del("Access-Control-Allow-Headers")
			resp.Header.Del("Access-Control-Allow-Methods")
			resp.Header.Del("Access-Control-Expose-Headers")
			resp.Header.Del("Access-Control-Allow-Credentials")

			// Not used right now.
			resp.Header.Del("Access-Control-Request-Private-Network")

			return nil
		}

		return proxy, nil
	}

	// Serve static files.
	static := http.FileServer(http.Dir("../build"))
	// Proxy static files to 3000, react dev server.
	proxy3000, err := httpCreateProxy("http://127.0.0.1:3000")
	if err != nil {
		return err
	}
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("AIT_PROXY_STATIC") == "true" {
			proxy3000.ServeHTTP(w, r)
		} else {
			static.ServeHTTP(w, r)
		}
	})

	// Setup the work dir.
	if pwd, err := os.Getwd(); err != nil {
		return errors.Wrapf(err, "getwd")
	} else {
		workDir = pwd
	}

	// Start HTTPS server.
	runHttpsServer := func() error {
		keyFile := path.Join(workDir, "../server.key")
		crtFile := path.Join(workDir, "../server.crt")

		var key, crt string
		generateCert := func() error {
			privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				return errors.Wrapf(err, "generate ecdsa key")
			}

			template := x509.Certificate{
				SerialNumber: big.NewInt(1),
				Subject: pkix.Name{
					CommonName: "srs.ai.talk",
				},
				NotBefore: time.Now(),
				NotAfter:  time.Now().AddDate(10, 0, 0),
				KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
				ExtKeyUsage: []x509.ExtKeyUsage{
					x509.ExtKeyUsageServerAuth,
				},
				BasicConstraintsValid: true,
			}

			derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
			if err != nil {
				return errors.Wrapf(err, "create certificate")
			}

			privateKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
			if err != nil {
				return errors.Wrapf(err, "marshal ecdsa key")
			}

			privateKeyBlock := pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: privateKeyBytes,
			}
			key = string(pem.EncodeToMemory(&privateKeyBlock))

			certBlock := pem.Block{
				Type:  "CERTIFICATE",
				Bytes: derBytes,
			}
			crt = string(pem.EncodeToMemory(&certBlock))
			logger.Tf(ctx, "cert: create self-signed certificate ok, key=%vB, crt=%vB", len(key), len(crt))

			return nil
		}

		if _, err := os.Stat(keyFile); os.IsNotExist(err) {
			if err := generateCert(); err != nil {
				return errors.Wrapf(err, "cert: create self-signed certificate failed")
			}

			if err := os.WriteFile(keyFile, []byte(key), 0644); err != nil {
				return errors.Wrapf(err, "cert: write key file failed")
			}
			if err := os.WriteFile(crtFile, []byte(crt), 0644); err != nil {
				return errors.Wrapf(err, "cert: write crt file failed")
			}
		}

		cert, err := tls.LoadX509KeyPair(crtFile, keyFile)
		if err != nil {
			return errors.Wrapf(err, "cert: ignore load cert %v, key %v failed", crtFile, keyFile)
		}

		addr := os.Getenv("AIT_HTTPS_LISTEN")
		if !strings.HasPrefix(addr, ":") {
			addr = fmt.Sprintf(":%v", addr)
		}
		logger.Tf(ctx, "HTTPS listen at %v", addr)

		server := &http.Server{
			Addr:    addr,
			Handler: handler,
			TLSConfig: &tls.Config{
				GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
					return &cert, nil
				},
			},
		}
		if err := server.ListenAndServeTLS("", ""); err != nil {
			return errors.Wrapf(err, "HTTPS Server error")
		}

		return nil
	}
	go func() {
		if err := runHttpsServer(); err != nil {
			logger.Ef(ctx, "HTTPS Server error: %+v", err)
		}
	}()

	// Start HTTP server.
	listen := os.Getenv("AIT_HTTP_LISTEN")
	if !strings.HasPrefix(listen, ":") {
		listen = fmt.Sprintf(":%v", listen)
	}
	logger.Tf(ctx, "Listen at %v, workDir=%v", listen, workDir)
	server := &http.Server{Addr: listen, Handler: handler}

	go func() {
		<-ctx.Done()
		server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return errors.Wrapf(err, "listen and serve")
	}
	logger.Tf(ctx, "HTTP Server closed")
	return nil
}

func doConfig(ctx context.Context) error {
	// setEnvDefault set env key=value if not set.
	setEnvDefault := func(key, value string) {
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}

	setEnvDefault("OPENAI_API_KEY", "")
	setEnvDefault("OPENAI_PROXY", "https://api.openai.com/v1")
	setEnvDefault("AIT_HTTP_LISTEN", "3001")
	setEnvDefault("AIT_HTTPS_LISTEN", "3443")
	setEnvDefault("AIT_PROXY_STATIC", "true")
	setEnvDefault("AIT_REPLY_PREFIX", "")
	setEnvDefault("AIT_SYSTEM_PROMPT", "You are a helpful assistant.")
	setEnvDefault("AIT_CHAT_MODEL", openai.GPT4TurboPreview)
	setEnvDefault("AIT_MAX_TOKENS", "1024")
	setEnvDefault("AIT_TEMPERATURE", "0.9")
	setEnvDefault("AIT_KEEP_FILES", "false")
	setEnvDefault("AIT_ASR_LANGUAGE", "en")
	setEnvDefault("AIT_REPLY_LIMIT", "30")
	setEnvDefault("AIT_DEVELOPMENT", "true")
	setEnvDefault("AIT_CHAT_WINDOW", "5")
	setEnvDefault("AIT_DEFAULT_ROBOT", "true")
	setEnvDefault("AIT_STAGE_TIMEOUT", "300")
	setEnvDefault("AIT_TTS_VOICE", string(openai.VoiceNova))
	setEnvDefault("AIT_TTS_MODEL", string(openai.TTSModel1))
	setEnvDefault("AIT_ASR_MODEL", openai.Whisper1)

	// Load env variables from file.
	if _, err := os.Stat("../.env"); err == nil {
		if err := godotenv.Overload("../.env"); err != nil {
			return errors.Wrapf(err, "load env")
		}
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		return errors.New("OPENAI_API_KEY is required")
	}

	logger.Tf(ctx, "OPENAI_API_KEY=%vB, OPENAI_PROXY=%v, AIT_HTTP_LISTEN=%v, AIT_HTTPS_LISTEN=%v, "+
		"AIT_PROXY_STATIC=%v, AIT_REPLY_PREFIX=%v, AIT_SYSTEM_PROMPT=%v, AIT_CHAT_MODEL=%v, AIT_MAX_TOKENS=%v, "+
		"AIT_TEMPERATURE=%v, AIT_KEEP_FILES=%v, AIT_ASR_LANGUAGE=%v, AIT_REPLY_LIMIT=%v, AIT_CHAT_WINDOW=%v, "+
		"AIT_DEFAULT_ROBOT=%v, AIT_STAGE_TIMEOUT=%v, AIT_TTS_VOICE=%v, AIT_TTS_MODEL=%v, "+
		"AIT_ASR_MODEL=%v",
		len(os.Getenv("OPENAI_API_KEY")), os.Getenv("OPENAI_PROXY"), os.Getenv("AIT_HTTP_LISTEN"),
		os.Getenv("AIT_HTTPS_LISTEN"), os.Getenv("AIT_PROXY_STATIC"), os.Getenv("AIT_REPLY_PREFIX"),
		os.Getenv("AIT_SYSTEM_PROMPT"), os.Getenv("AIT_CHAT_MODEL"), os.Getenv("AIT_MAX_TOKENS"),
		os.Getenv("AIT_TEMPERATURE"), os.Getenv("AIT_KEEP_FILES"), os.Getenv("AIT_ASR_LANGUAGE"),
		os.Getenv("AIT_REPLY_LIMIT"), os.Getenv("AIT_CHAT_WINDOW"),
		os.Getenv("AIT_DEFAULT_ROBOT"), os.Getenv("AIT_STAGE_TIMEOUT"), os.Getenv("AIT_TTS_VOICE"),
		os.Getenv("AIT_TTS_MODEL"), os.Getenv("AIT_ASR_MODEL"),
	)

	// Config all robots.
	globalReplylimit, err := strconv.ParseInt(os.Getenv("AIT_REPLY_LIMIT"), 10, 64)
	if err != nil {
		return errors.Wrapf(err, "parse AIT_REPLY_LIMIT %v", os.Getenv("AIT_REPLY_LIMIT"))
	}

	globalChatWindow, err := strconv.ParseInt(os.Getenv("AIT_CHAT_WINDOW"), 10, 64)
	if err != nil {
		return errors.Wrapf(err, "parse AIT_CHAT_WINDOW %v", os.Getenv("AIT_CHAT_WINDOW"))
	}

	if os.Getenv("AIT_DEFAULT_ROBOT") == "true" {
		robots = append(robots, &Robot{
			uuid: "default", label: "Default", prompt: os.Getenv("AIT_SYSTEM_PROMPT"),
			asrLanguage: os.Getenv("AIT_ASR_LANGUAGE"), prefix: os.Getenv("AIT_REPLY_PREFIX"),
			voice: "hello-english.aac", replyLimit: int(globalReplylimit),
			chatModel: os.Getenv("AIT_CHAT_MODEL"), chatWindow: int(globalChatWindow),
		})
	}

	for i := 0; i < 100; i++ {
		uuid := os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_ID", i))
		label := os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_LABEL", i))
		prompt := os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_PROMPT", i))
		if uuid == "" || label == "" || prompt == "" {
			if uuid != "" || label != "" || prompt != "" {
				logger.Wf(ctx, "Ignore uuid=%v, label=%v, prompt=%v", uuid, label, prompt)
			}
			continue
		}

		setEnvDefault(fmt.Sprintf("AIT_ROBOT_%v_ASR_LANGUAGE", i), os.Getenv("AIT_ASR_LANGUAGE"))
		setEnvDefault(fmt.Sprintf("AIT_ROBOT_%v_REPLY_PREFIX", i), os.Getenv("AIT_REPLY_PREFIX"))

		voice := "hello-english.aac"
		if os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_ASR_LANGUAGE", i)) == "zh" {
			voice = "hello-chinese.aac"
		}

		replyLimit := int(globalReplylimit)
		if os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_REPLY_LIMIT", i)) != "" {
			if iv, err := strconv.ParseInt(os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_REPLY_LIMIT", i)), 10, 64); err != nil {
				return errors.Wrapf(err, "parse AIT_REPLY_LIMIT %v", os.Getenv("AIT_REPLY_LIMIT"))
			} else {
				replyLimit = int(iv)
			}
		}

		chatModel := os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_CHAT_MODEL", i))
		if chatModel == "" {
			chatModel = os.Getenv("AIT_CHAT_MODEL")
		}

		chatWindow := int(globalChatWindow)
		if os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_CHAT_WINDOW", i)) != "" {
			if iv, err := strconv.ParseInt(os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_CHAT_WINDOW", i)), 10, 64); err != nil {
				return errors.Wrapf(err, "parse AIT_CHAT_WINDOW %v", os.Getenv("AIT_CHAT_WINDOW"))
			} else {
				chatWindow = int(iv)
			}
		}

		prefix := os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_REPLY_PREFIX", i))
		asrLanguage := os.Getenv(fmt.Sprintf("AIT_ROBOT_%v_ASR_LANGUAGE", i))

		robots = append(robots, &Robot{
			uuid: uuid, label: label, prompt: prompt, asrLanguage: asrLanguage, prefix: prefix,
			voice: voice, replyLimit: replyLimit, chatModel: chatModel, chatWindow: chatWindow,
		})
	}

	var sb []string
	for i, robot := range robots {
		sb = append(sb, fmt.Sprintf("#%v=<%v>", i, robot.String()))
	}
	logger.Tf(ctx, "Robots: total=%v, %v", len(robots), strings.Join(sb, ", "))

	// Initialize OpenAI client config.
	openaiInit(ctx)
	tencentInit(ctx)

	return nil
}

func main() {
	ctx := context.Background()
	if err := doMain(ctx); err != nil {
		logger.Ef(ctx, "Main error: %+v", err)
		os.Exit(-1)
	}
}
