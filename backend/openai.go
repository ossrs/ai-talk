package main

import (
	"context"
	"encoding/json"
	errors_std "errors"
	"fmt"
	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"
	"github.com/sashabaranov/go-openai"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var asrAIConfig openai.ClientConfig
var chatAIConfig openai.ClientConfig
var ttsAIConfig openai.ClientConfig

func openaiInit(ctx context.Context) {
	filterProxyUrl := func(proxy string) string {
		var baseURL string
		if strings.Contains(proxy, "://") {
			baseURL = proxy
		} else {
			baseURL = fmt.Sprintf("https://%v", proxy)
		}

		if !strings.HasSuffix(baseURL, "/v1") {
			baseURL = fmt.Sprintf("%v/v1", baseURL)
		}
		return baseURL
	}
	getFirstEnv := func(envNames ...string) string {
		for _, envName := range envNames {
			if v := os.Getenv(envName); v != "" {
				return v
			}
		}
		return ""
	}

	asrAPIKey := getFirstEnv("ASR_OPENAI_API_KEY", "OPENAI_API_KEY")
	asrPorxy := getFirstEnv("ASR_OPENAI_PROXY", "OPENAI_PROXY")
	asrAIConfig = openai.DefaultConfig(asrAPIKey)
	asrAIConfig.BaseURL = filterProxyUrl(asrPorxy)

	chatAPIKey := getFirstEnv("CHAT_OPENAI_API_KEY", "OPENAI_API_KEY")
	chatPorxy := getFirstEnv("CHAT_OPENAI_PROXY", "OPENAI_PROXY")
	chatAIConfig = openai.DefaultConfig(chatAPIKey)
	chatAIConfig.BaseURL = filterProxyUrl(chatPorxy)

	ttsAPIKey := getFirstEnv("TTS_OPENAI_API_KEY", "OPENAI_API_KEY")
	ttsPorxy := getFirstEnv("TTS_OPENAI_PROXY", "OPENAI_PROXY")
	ttsAIConfig = openai.DefaultConfig(ttsAPIKey)
	ttsAIConfig.BaseURL = filterProxyUrl(ttsPorxy)

	logger.Tf(ctx, "OpenAI config, asr<key=%vB, proxy=%v, base=%v>, chat=<key=%vB, proxy=%v, base=%v>, tts=<key=%vB, proxy=%v, base=%v>",
		len(asrAPIKey), asrPorxy, asrAIConfig.BaseURL,
		len(chatAPIKey), chatPorxy, chatAIConfig.BaseURL,
		len(ttsAPIKey), ttsPorxy, ttsAIConfig.BaseURL,
	)
}

type openaiASRService struct {
}

func NewOpenAIASRService(opts ...func(service *openaiASRService)) ASRService {
	v := &openaiASRService{}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func (v *openaiASRService) RequestASR(ctx context.Context, inputFile, language, prompt string, onBeforeRequest func()) (*ASRResult, error) {
	outputFile := fmt.Sprintf("%v.mp4", inputFile)
	if os.Getenv("AIT_KEEP_FILES") != "true" {
		defer os.Remove(outputFile)
	}

	// Transcode input audio in opus or aac, to aac in m4a format.
	// If need to encode to aac, use:
	//		"-c:a", "aac", "-ac", "1", "-ar", "16000", "-ab", "30k",
	if err := exec.CommandContext(ctx, "ffmpeg",
		"-i", inputFile,
		"-vn", "-c:a", "copy",
		outputFile,
	).Run(); err != nil {
		return nil, errors.Errorf("Error converting the file")
	}
	logger.Tf(ctx, "Convert audio %v to %v ok", inputFile, outputFile)

	if onBeforeRequest != nil {
		onBeforeRequest()
	}

	// Request ASR.
	client := openai.NewClientWithConfig(asrAIConfig)
	resp, err := client.CreateTranscription(
		ctx,
		openai.AudioRequest{
			Model:    os.Getenv("AIT_ASR_MODEL"),
			FilePath: outputFile,
			// Note that must use verbose JSON, to get the duration of file.
			Format:   openai.AudioResponseFormatVerboseJSON,
			Language: language,
			Prompt:   prompt,
		},
	)
	if err != nil {
		return nil, errors.Wrapf(err, "asr")
	}

	return &ASRResult{Text: resp.Text, Duration: time.Duration(resp.Duration * float64(time.Second))}, nil
}

func ffprobeAudio(ctx context.Context, filename string) (duration float64, bitrate int, err error) {
	args := []string{
		"-show_error", "-show_private_data", "-v", "quiet", "-find_stream_info", "-print_format", "json",
		"-show_format",
	}
	args = append(args, "-i", filename)

	stdout, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if err != nil {
		err = errors.Wrapf(err, "probe %v", filename)
		return
	}

	type VLiveFileFormat struct {
		Starttime string `json:"start_time"`
		Duration  string `json:"duration"`
		Bitrate   string `json:"bit_rate"`
		Streams   int32  `json:"nb_streams"`
		Score     int32  `json:"probe_score"`
		HasVideo  bool   `json:"has_video"`
		HasAudio  bool   `json:"has_audio"`
	}

	format := struct {
		Format VLiveFileFormat `json:"format"`
	}{}
	if err = json.Unmarshal([]byte(stdout), &format); err != nil {
		err = errors.Wrapf(err, "parse format %v", stdout)
		return
	}

	var fv float64
	if fv, err = strconv.ParseFloat(format.Format.Duration, 64); err != nil {
		err = errors.Wrapf(err, "parse duration %v", format.Format.Duration)
		return
	} else {
		duration = fv
	}

	var iv int64
	if iv, err = strconv.ParseInt(format.Format.Bitrate, 10, 64); err != nil {
		err = errors.Wrapf(err, "parse bitrate %v", format.Format.Bitrate)
		return
	} else {
		bitrate = int(iv)
	}

	logger.Tf(ctx, "FFprobe input=%v, duration=%v, bitrate=%v", filename, duration, bitrate)
	return
}

type openaiChatService struct {
	onFirstResponse func(ctx context.Context, text string)
}

func (v *openaiChatService) RequestChat(ctx context.Context, rid string, stage *Stage, robot *Robot) error {
	if stage.previousUser != "" && stage.previousAssitant != "" {
		stage.histories = append(stage.histories, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: stage.previousUser,
		}, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: stage.previousAssitant,
		})

		for len(stage.histories) > robot.chatWindow*2 {
			stage.histories = stage.histories[1:]
		}
	}

	stage.previousUser = stage.previousAsrText
	stage.previousAssitant = ""

	system := robot.prompt
	system += fmt.Sprintf(" Keep your reply neat, limiting the reply to %v words.", robot.replyLimit)
	logger.Tf(ctx, "AI system prompt: %v", system)
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: system},
	}

	messages = append(messages, stage.histories...)
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: stage.previousAsrText,
	})

	model := robot.chatModel
	var maxTokens int
	if v, err := strconv.ParseInt(os.Getenv("AIT_MAX_TOKENS"), 10, 64); err != nil {
		return errors.Wrapf(err, "parse AIT_MAX_TOKENS %v", os.Getenv("AIT_MAX_TOKENS"))
	} else {
		maxTokens = int(v)
	}

	var temperature float32
	if v, err := strconv.ParseFloat(os.Getenv("AIT_TEMPERATURE"), 64); err != nil {
		return errors.Wrapf(err, "parse AIT_TEMPERATURE %v", os.Getenv("AIT_TEMPERATURE"))
	} else {
		temperature = float32(v)
	}
	logger.Tf(ctx, "robot=%v(%v), OPENAI_PROXY: %v, AIT_CHAT_MODEL: %v, AIT_MAX_TOKENS: %v, AIT_TEMPERATURE: %v, window=%v, histories=%v",
		robot.uuid, robot.label, chatAIConfig.BaseURL, model, maxTokens, temperature, robot.chatWindow, len(stage.histories))

	client := openai.NewClientWithConfig(chatAIConfig)
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

	// Never wait for any response.
	go func() {
		defer gptChatStream.Close()
		if err := v.handle(ctx, stage, robot, rid, gptChatStream); err != nil {
			logger.Ef(ctx, "Handle stream failed, err %+v", err)
		}
	}()

	return nil
}

func (v *openaiChatService) handle(ctx context.Context, stage *Stage, robot *Robot, rid string, gptChatStream *openai.ChatCompletionStream) error {
	stage.generating = true
	defer func() {
		stage.generating = false
	}()

	filterAIResponse := func(response *openai.ChatCompletionStreamResponse, err error) (bool, string, error) {
		finished := errors_std.Is(err, io.EOF)
		if err != nil && !finished {
			return finished, "", errors.Wrapf(err, "recv chat")
		}

		if len(response.Choices) == 0 {
			return finished, "", nil
		}

		choice := response.Choices[0]
		dc := choice.Delta.Content
		if dc == "" {
			return finished, "", nil
		}

		filteredStencese := strings.ReplaceAll(dc, "\n\n", "\n")
		filteredStencese = strings.ReplaceAll(filteredStencese, "\n", " ")

		return finished, filteredStencese, nil
	}

	gotNewSentence := func(sentence, lastWords string, firstSentense bool) bool {
		newSentence := false

		isEnglish := func(s string) bool {
			for _, r := range s {
				if r > unicode.MaxASCII {
					return false
				}
			}
			return true
		}

		// Ignore empty.
		if sentence == "" {
			return newSentence
		}

		// Any ASCII character to split sentence.
		if strings.ContainsAny(lastWords, ",.?!\n") {
			newSentence = true
		}

		// Any Chinese character to split sentence.
		if strings.ContainsRune(lastWords, '。') ||
			strings.ContainsRune(lastWords, '？') ||
			strings.ContainsRune(lastWords, '！') ||
			strings.ContainsRune(lastWords, '，') {
			newSentence = true
		}

		// Badcase, for number such as 1.3, or 1,300,000.
		var badcase bool
		if match, _ := regexp.MatchString(`\d+(\.|,)\d*$`, sentence); match {
			badcase, newSentence = true, false
		}

		// Determine whether new sentence by length.
		if isEnglish(sentence) {
			maxWords, minWords := 30, 3
			if !firstSentense || badcase {
				maxWords, minWords = 50, 5
			}

			if nn := strings.Count(sentence, " "); nn >= maxWords {
				newSentence = true
			} else if nn < minWords {
				newSentence = false
			}
		} else {
			maxWords, minWords := 50, 3
			if !firstSentense || badcase {
				maxWords, minWords = 100, 5
			}

			if nn := utf8.RuneCount([]byte(sentence)); nn >= maxWords {
				newSentence = true
			} else if nn < minWords {
				newSentence = false
			}
		}

		return newSentence
	}

	commitAISentence := func(sentence string, firstSentense bool) {
		if sentence == "" {
			return
		}

		if firstSentense {
			if robot.prefix != "" {
				sentence = fmt.Sprintf("%v %v", robot.prefix, sentence)
			}
			if v.onFirstResponse != nil {
				v.onFirstResponse(ctx, sentence)
			}
		}

		stage.ttsWorker.SubmitSegment(ctx, stage, NewAnswerSegment(func(segment *AnswerSegment) {
			segment.rid = rid
			segment.text = sentence
			segment.first = firstSentense
		}))
		return
	}

	var sentence, lastWords string
	isFinished, firstSentense := false, true
	for !isFinished && ctx.Err() == nil {
		response, err := gptChatStream.Recv()
		if finished, words, err := filterAIResponse(&response, err); err != nil {
			return errors.Wrapf(err, "filter")
		} else {
			isFinished, sentence, lastWords = finished, sentence+words, words
		}
		logger.Tf(ctx, "AI response: text=%v plus %v", lastWords, sentence)

		newSentence := gotNewSentence(sentence, lastWords, firstSentense)
		if !isFinished && !newSentence {
			continue
		}

		// Use the sentence for prompt and logging.
		stage.previousAssitant += sentence + " "
		// We utilize user ASR and AI responses as prompts for the subsequent ASR, given that this is
		// a chat-based scenario where the user converses with the AI, and the following audio should pertain to both user and AI text.
		stage.previousAsrText += " " + sentence
		// Commit the sentense to TTS worker and callbacks.
		commitAISentence(sentence, firstSentense)
		// Reset the sentence, because we have committed it.
		sentence, firstSentense = "", false
	}

	return nil
}

type openaiTTSService struct {
}

func NewOpenAITTSService() TTSService {
	return &openaiTTSService{}
}

func (v *openaiTTSService) RequestTTS(ctx context.Context, buildFilepath func(ext string) string, text string) error {
	ttsFile := buildFilepath("aac")

	client := openai.NewClientWithConfig(ttsAIConfig)
	resp, err := client.CreateSpeech(ctx, openai.CreateSpeechRequest{
		Model:          openai.SpeechModel(os.Getenv("AIT_TTS_MODEL")),
		Input:          text,
		Voice:          openai.SpeechVoice(os.Getenv("AIT_TTS_VOICE")),
		ResponseFormat: openai.SpeechResponseFormatAac,
	})
	if err != nil {
		return errors.Wrapf(err, "create speech")
	}
	defer resp.Close()

	out, err := os.Create(ttsFile)
	if err != nil {
		return errors.Errorf("Unable to create the file %v for writing", ttsFile)
	}
	defer out.Close()

	if _, err = io.Copy(out, resp); err != nil {
		return errors.Errorf("Error writing the file")
	}

	return nil
}
