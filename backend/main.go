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
	errors_std "errors"
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
	"os/exec"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

var ttsWorker TTSWorker
var previousAsrText string
var previousUser, previousAssitant string
var histories []openai.ChatCompletionMessage
var aiConfig openai.ClientConfig
var workDir string

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
}

// The TTSWorker is a worker to convert answers from text to audio.
type TTSWorker struct {
	segments []*AnswerSegment
	lock     sync.Mutex
	wg       sync.WaitGroup
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

func (v *TTSWorker) QueryAnyReadySegment(ctx context.Context, rid string) *AnswerSegment {
	for ctx.Err() == nil {
		var s *AnswerSegment

		// When there is no segments, maybe AI is generating the sentence, we need to wait. For example,
		// if the first sentence is very short, maybe we got it quickly, but the second sentence is very
		// long so that the AI need more time to generate it.
		for i := 0; i < 10 && s == nil; i++ {
			if s = v.query(rid); s == nil {
				select {
				case <-ctx.Done():
				case <-time.After(200 * time.Millisecond):
				}
			}
		}

		if s == nil {
			return nil
		}

		if !s.dummy && (s.ready || s.err != nil) {
			return s
		}

		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
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

func (v *TTSWorker) SubmitSegment(ctx context.Context, segment *AnswerSegment) {
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

		if err := func() error {
			client := openai.NewClientWithConfig(aiConfig)
			resp, err := client.CreateSpeech(ctx, openai.CreateSpeechRequest{
				Model:          openai.TTSModel1,
				Input:          segment.text,
				Voice:          openai.VoiceNova,
				ResponseFormat: openai.SpeechResponseFormatAac,
			})
			if err != nil {
				return errors.Wrapf(err, "create speech")
			}
			defer resp.Close()

			out, err := os.Create(segment.ttsFile)
			if err != nil {
				return errors.Errorf("Unable to create the file %v for writing", segment.ttsFile)
			}
			defer out.Close()

			nn, err := io.Copy(out, resp)
			if err != nil {
				return errors.Errorf("Error writing the file")
			}

			segment.ready = true
			logger.Tf(ctx, "File saved to %v, size: %v, %v", segment.ttsFile, nn, segment.text)
			return nil
		}(); err != nil {
			segment.err = err
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

			ttsWorker.RemoveSegment(segment.asid)

			if segment.ttsFile != "" && os.Getenv("KEEP_AUDIO_FILES") != "true" {
				if _, err := os.Stat(segment.ttsFile); err == nil {
					os.Remove(segment.ttsFile)
				}
			}
		}()
	}()
}

// When user start a scenario, response a scenario object, which identified by sid or scenario id.
func handleScenarioStart(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	return nil
}

// When user ask a question, which is a request with audio, which is identified by rid (request id).
func handleUploadQuestionAudio(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	handleChatResponseStream := func(ctx context.Context, rid string, gptChatStream *openai.ChatCompletionStream) error {
		var sentence string
		var finished bool
		firstSentense := true
		for !finished && ctx.Err() == nil {
			response, err := gptChatStream.Recv()
			finished = errors_std.Is(err, io.EOF)
			if err != nil && !finished {
				return errors.Wrapf(err, "recv chat")
			}

			newSentence := false
			if len(response.Choices) > 0 {
				choice := response.Choices[0]
				if dc := choice.Delta.Content; dc != "" {
					filteredStencese := strings.ReplaceAll(dc, "\n\n", "\n")
					filteredStencese = strings.ReplaceAll(filteredStencese, "\n", " ")
					sentence += filteredStencese

					// Any ASCII character to split sentence.
					if strings.ContainsAny(dc, ",.?!\n") {
						newSentence = true
					}

					// Any Chinese character to split sentence.
					if strings.ContainsRune(dc, '。') ||
						strings.ContainsRune(dc, '？') ||
						strings.ContainsRune(dc, '！') {
						newSentence = true
					}
				}
			}

			if sentence == "" {
				continue
			}

			isEnglish := func(s string) bool {
				for _, r := range s {
					if r > unicode.MaxASCII {
						return false
					}
				}
				return true
			}

			// Determine whether new sentence by length.
			if isEnglish(sentence) {
				if nn := strings.Count(sentence, " "); nn >= 30 {
					newSentence = true
				} else if nn < 3 {
					newSentence = false
				}
			} else {
				if nn := utf8.RuneCount([]byte(sentence)); nn >= 50 {
					newSentence = true
				} else if nn < 3 {
					newSentence = false
				}
			}

			if finished || newSentence {
				previousAssitant += sentence + " "

				if firstSentense {
					firstSentense = false
					if os.Getenv("AI_NO_PADDING") != "true" {
						sentence = fmt.Sprintf("%v%v", os.Getenv("AI_PADDING_TEXT"), sentence)
					}
				}

				s := &AnswerSegment{
					rid:          rid,
					asid:         uuid.NewString(),
					text:         sentence,
					removeSignal: make(chan bool, 1),
				}
				s.ttsFile = path.Join(workDir, fmt.Sprintf("%v-sentence-%v-tts.aac", rid, s.asid))
				sentence = ""

				ttsWorker.SubmitSegment(ctx, s)
			}
		}

		return nil
	}

	ctx = logger.WithContext(ctx)

	// The rid is the request id, which identify this request, generally a question.
	rid := uuid.NewString()

	// We save the input audio to *.audio file, it can be aac or opus codec.
	inputFile := path.Join(workDir, fmt.Sprintf("%v-input.audio", rid))
	if os.Getenv("KEEP_AUDIO_FILES") != "true" {
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

	outputFile := path.Join(workDir, fmt.Sprintf("%v-input.m4a", rid))
	if os.Getenv("KEEP_AUDIO_FILES") != "true" {
		defer os.Remove(outputFile)
	}
	if true {
		err := exec.CommandContext(ctx, "ffmpeg",
			"-i", inputFile,
			"-vn", "-c:a", "aac", "-ac", "1", "-ar", "16000", "-ab", "30k",
			outputFile,
		).Run()

		if err != nil {
			return errors.Errorf("Error converting the file")
		}
		logger.Tf(ctx, "Convert to ogg %v ok", outputFile)
	}

	var config openai.ClientConfig
	config = openai.DefaultConfig(os.Getenv("OPENAI_API_KEY"))
	if os.Getenv("OPENAI_PROXY") != "" {
		config.BaseURL = fmt.Sprintf("http://%v/v1", os.Getenv("OPENAI_PROXY"))
	}

	client := openai.NewClientWithConfig(config)
	resp, err := client.CreateTranscription(
		ctx,
		openai.AudioRequest{
			Model:    openai.Whisper1,
			FilePath: outputFile,
			Format:   openai.AudioResponseFormatJSON,
			Language: os.Getenv("AI_ASR_LANGUAGE"),
			Prompt:   previousAsrText,
		},
	)
	if err != nil {
		return errors.Wrapf(err, "transcription")
	}
	logger.Tf(ctx, "ASR ok, lang=%v, prompt=<%v>, resp is <%v>",
		os.Getenv("AI_ASR_LANGUAGE"), previousAsrText, resp.Text)
	asrText := resp.Text
	previousAsrText = resp.Text
	fmt.Fprintf(os.Stderr, fmt.Sprintf("You: %v\n", asrText))

	if previousUser != "" && previousAssitant != "" {
		histories = append(histories, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: previousUser,
		}, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: previousAssitant,
		})
		for len(histories) > 10 {
			histories = histories[1:]
		}
	}

	previousUser = previousAsrText
	previousAssitant = ""

	system := os.Getenv("AI_SYSTEM_PROMPT")
	logger.Tf(ctx, "AI system prompt(AI_SYSTEM_PROMPT): %v", system)
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: system},
	}

	messages = append(messages, histories...)
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: previousAsrText,
	})

	model := os.Getenv("AI_MODEL")
	var maxTokens int
	if v, err := strconv.ParseInt(os.Getenv("AI_MAX_TOKENS"), 10, 64); err != nil {
		return errors.Wrapf(err, "parse AI_MAX_TOKENS")
	} else {
		maxTokens = int(v)
	}

	var temperature float32
	if v, err := strconv.ParseFloat(os.Getenv("AI_TEMPERATURE"), 64); err != nil {
		return errors.Wrapf(err, "parse AI_TEMPERATURE")
	} else {
		temperature = float32(v)
	}
	logger.Tf(ctx, "AI_MODEL: %v, AI_MAX_TOKENS: %v, AI_TEMPERATURE: %v",
		model, maxTokens, temperature)

	gptChatStream, err := client.CreateChatCompletionStream(
		ctx, openai.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			Stream:      true,
			Temperature: temperature,
			MaxTokens:   maxTokens,
		},
	)
	if err != nil {
		return errors.Wrapf(err, "create chat")
	}

	// Insert a dummy sentence to identify the request is alive.
	ttsWorker.SubmitSegment(ctx, &AnswerSegment{
		rid:   rid,
		asid:  uuid.NewString(),
		dummy: true,
	})

	// Never wait for any response.
	go func() {
		defer gptChatStream.Close()
		if err := handleChatResponseStream(ctx, rid, gptChatStream); err != nil {
			logger.Ef(ctx, "Handle stream failed, err %+v", err)
		}
	}()

	// Response the request UUID and pulling the response.
	ohttp.WriteData(ctx, w, r, struct {
		RequestUUID string `json:"rid"`
		ASR         string `json:"asr"`
	}{
		RequestUUID: rid,
		ASR:         asrText,
	})
	return nil
}

// When user query the question state, which is identified by rid (request id).
func handleQueryQuestionState(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// The rid is the request id, which identify this request, generally a question.
	rid := r.URL.Query().Get("rid")
	if rid == "" {
		return errors.Errorf("empty rid")
	}

	segment := ttsWorker.QueryAnyReadySegment(ctx, rid)
	if segment == nil {
		ohttp.WriteData(ctx, w, r, struct {
			AnswerSegmentUUID string `json:"asid"`
		}{})
		return nil
	}

	ohttp.WriteData(ctx, w, r, struct {
		Processing        bool   `json:"processing"`
		AnswerSegmentUUID string `json:"asid"`
		TTS               string `json:"tts"`
	}{
		Processing:        segment.dummy || (!segment.ready && segment.err == nil),
		AnswerSegmentUUID: segment.asid,
		TTS:               segment.text,
	})
	return nil
}

// When user download the answer tts, which is identified by rid (request id) and aid (answer id)
func handleDownloadAnswerTTS(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	rid := r.URL.Query().Get("rid")
	if rid == "" {
		return errors.Errorf("empty rid")
	}

	asid := r.URL.Query().Get("asid")
	if asid == "" {
		return errors.Errorf("empty asid")
	}

	segment := ttsWorker.QuerySegment(rid, asid)
	if segment == nil {
		return errors.Errorf("no segment for %v %v", rid, asid)
	}
	logger.Tf(ctx, "Query segment %v %v, dummy=%v, segment=%v, err=%v",
		rid, asid, segment.dummy, segment.text, segment.err)

	fmt.Fprintf(os.Stderr, "Bot: %v\n", segment.text)

	// Read the ttsFile and response it as opus audio.
	w.Header().Set("Content-Type", "audio/aac")
	http.ServeFile(w, r, segment.ttsFile)

	return nil
}

// When user remove the answer tts, which is identified by rid (request id) and aid (answer id)
func handleRemoveAnswerTTS(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	rid := r.URL.Query().Get("rid")
	if rid == "" {
		return errors.Errorf("empty rid")
	}

	asid := r.URL.Query().Get("asid")
	if asid == "" {
		return errors.Errorf("empty asid")
	}

	segment := ttsWorker.QuerySegment(rid, asid)
	if segment == nil {
		return errors.Errorf("no segment for %v %v", rid, asid)
	}

	// Remove it.
	ttsWorker.RemoveSegment(asid)

	select {
	case <-ctx.Done():
	case segment.removeSignal <- true:
	}

	ohttp.WriteData(ctx, w, r, nil)
	return nil
}

func doMain(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Cleanup TTS worker.
	defer ttsWorker.Close()

	// Signal handler.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		logger.Tf(ctx, "Got signal %v", sig)
		cancel()
	}()

	// Load env variables from file.
	if _, err := os.Stat("../.env"); err == nil {
		if err := godotenv.Load("../.env"); err != nil {
			return errors.Wrapf(err, "load env")
		}
	} else {
		if os.Getenv("OPENAI_API_KEY") == "" {
			return errors.Wrapf(err, "not found .env")
		}
	}

	// setEnvDefault set env key=value if not set.
	setEnvDefault := func(key, value string) {
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	setEnvDefault("OPENAI_PROXY", "api.openai.com")
	setEnvDefault("HTTP_LISTEN", "3001")
	setEnvDefault("HTTPS_LISTEN", "3443")
	setEnvDefault("PROXY_STATIC", "true")
	setEnvDefault("AI_NO_PADDING", "true")
	setEnvDefault("AI_PADDING_TEXT", "My answer is ")
	setEnvDefault("AI_SYSTEM_PROMPT", "You are a helpful assistant.")
	setEnvDefault("AI_MODEL", openai.GPT4TurboPreview)
	setEnvDefault("AI_MAX_TOKENS", "1024")
	setEnvDefault("AI_TEMPERATURE", "0.9")
	setEnvDefault("KEEP_AUDIO_FILES", "false")
	setEnvDefault("AI_ASR_LANGUAGE", "en")
	logger.Tf(ctx, "OPENAI_API_KEY=%vB, OPENAI_PROXY=%v, HTTP_LISTEN=%v, HTTPS_LISTEN=%v, PROXY_STATIC=%v, "+
		"AI_NO_PADDING=%v, AI_PADDING_TEXT=%v, AI_SYSTEM_PROMPT=%v, AI_MODEL=%v, AI_MAX_TOKENS=%v, AI_TEMPERATURE=%v, "+
		"KEEP_AUDIO_FILES=%v, AI_ASR_LANGUAGE=%v",
		len(os.Getenv("OPENAI_API_KEY")), os.Getenv("OPENAI_PROXY"), os.Getenv("HTTP_LISTEN"),
		os.Getenv("HTTPS_LISTEN"), os.Getenv("PROXY_STATIC"), os.Getenv("AI_NO_PADDING"),
		os.Getenv("AI_PADDING_TEXT"), os.Getenv("AI_SYSTEM_PROMPT"), os.Getenv("AI_MODEL"),
		os.Getenv("AI_MAX_TOKENS"), os.Getenv("AI_TEMPERATURE"), os.Getenv("KEEP_AUDIO_FILES"),
		os.Getenv("AI_ASR_LANGUAGE"),
	)

	// Initialize OpenAI client config.
	aiConfig = openai.DefaultConfig(os.Getenv("OPENAI_API_KEY"))
	if proxy := os.Getenv("OPENAI_PROXY"); proxy != "" {
		if strings.Contains(proxy, "://") {
			aiConfig.BaseURL = proxy
		} else if strings.Contains(proxy, "openai.com") {
			aiConfig.BaseURL = fmt.Sprintf("http://%v", proxy)
		} else {
			aiConfig.BaseURL = fmt.Sprintf("http://%v", proxy)
		}

		if !strings.HasSuffix(aiConfig.BaseURL, "/v1") {
			aiConfig.BaseURL = fmt.Sprintf("%v/v1", aiConfig.BaseURL)
		}
	}
	logger.Tf(ctx, "OpenAI key(OPENAI_API_KEY): %vB, proxy(OPENAI_PROXY): %v, base url: %v",
		len(os.Getenv("OPENAI_API_KEY")), os.Getenv("OPENAI_PROXY"), aiConfig.BaseURL)

	// HTTP API handlers.
	handler := http.NewServeMux()

	handler.HandleFunc("/api/ai-talk/start/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleScenarioStart(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle start failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler.HandleFunc("/api/ai-talk/upload/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleUploadQuestionAudio(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle audio failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	handler.HandleFunc("/api/ai-talk/question/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleQueryQuestionState(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle question failed, err %+v", err)
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
		filename := r.URL.Path[len("/api/ai-talk/examples/"):]
		if !strings.Contains(filename, ".") {
			filename = fmt.Sprintf("%v.aac", filename)
		}

		ext := strings.Trim(path.Ext(filename), ".")
		contentType := fmt.Sprintf("audio/%v", ext)
		logger.Tf(ctx, "Serve example file=%v, ext=%v, contentType=%v", filename, ext, contentType)

		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, path.Join(workDir, filename))
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
		if os.Getenv("PROXY_STATIC") == "true" {
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

		addr := os.Getenv("HTTPS_LISTEN")
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
	listen := os.Getenv("HTTP_LISTEN")
	if !strings.HasPrefix(listen, ":") {
		listen = fmt.Sprintf(":%v", listen)
	}
	fmt.Fprintf(os.Stderr, fmt.Sprintf("Listen at %v, workDir=%v\n", listen, workDir))
	logger.Tf(ctx, "Listen at %v", listen)
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

func main() {
	ctx := context.Background()
	if err := doMain(ctx); err != nil {
		logger.Ef(ctx, "Main error: %+v", err)
		os.Exit(-1)
	}
}
