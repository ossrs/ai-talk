package main

import (
	"context"
	errors_std "errors"
	"fmt"
	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"
	"github.com/sashabaranov/go-openai"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var aiConfig openai.ClientConfig

func openaiInit(ctx context.Context) {
	aiConfig = openai.DefaultConfig(os.Getenv("OPENAI_API_KEY"))
	if proxy := os.Getenv("OPENAI_PROXY"); proxy != "" {
		if strings.Contains(proxy, "://") {
			aiConfig.BaseURL = proxy
		} else {
			aiConfig.BaseURL = fmt.Sprintf("https://%v", proxy)
		}

		if !strings.HasSuffix(aiConfig.BaseURL, "/v1") {
			aiConfig.BaseURL = fmt.Sprintf("%v/v1", aiConfig.BaseURL)
		}
	}
	logger.Tf(ctx, "OpenAI key(OPENAI_API_KEY): %vB, proxy(OPENAI_PROXY): %v, base url: %v",
		len(os.Getenv("OPENAI_API_KEY")), os.Getenv("OPENAI_PROXY"), aiConfig.BaseURL)
}

type openaiASRService struct {
}

func NewOpenAIASRService() ASRService {
	return &openaiASRService{}
}

func (v *openaiASRService) RequestASR(ctx context.Context, inputFile, language, prompt string) (string, error) {
	outputFile := fmt.Sprintf("%v.m4a", inputFile)

	// Transcode input audio in opus or aac, to aac in m4a format.
	if os.Getenv("AIT_KEEP_FILES") != "true" {
		defer os.Remove(outputFile)
	}
	if true {
		err := exec.CommandContext(ctx, "ffmpeg",
			"-i", inputFile,
			"-vn", "-c:a", "aac", "-ac", "1", "-ar", "16000", "-ab", "50k",
			outputFile,
		).Run()

		if err != nil {
			return "", errors.Errorf("Error converting the file")
		}
		logger.Tf(ctx, "Convert audio %v to %v ok", inputFile, outputFile)
	}

	client := openai.NewClientWithConfig(aiConfig)
	resp, err := client.CreateTranscription(
		ctx,
		openai.AudioRequest{
			Model:    os.Getenv("AIT_ASR_MODEL"),
			FilePath: outputFile,
			Format:   openai.AudioResponseFormatJSON,
			Language: language,
			Prompt:   prompt,
		},
	)
	if err != nil {
		return "", errors.Wrapf(err, "asr")
	}

	return resp.Text, nil
}

type openaiChatService struct {
	onFirstResponse func(ctx context.Context)
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
	logger.Tf(ctx, "robot=%v(%v), AIT_CHAT_MODEL: %v, AIT_MAX_TOKENS: %v, AIT_TEMPERATURE: %v, window=%v, histories=%v",
		robot.uuid, robot.label, model, maxTokens, temperature, robot.chatWindow, len(stage.histories))

	client := openai.NewClientWithConfig(aiConfig)
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
			maxWords, minWords := 30, 3
			if !firstSentense {
				maxWords, minWords = 50, 10
			}

			if nn := strings.Count(sentence, " "); nn >= maxWords {
				newSentence = true
			} else if nn < minWords {
				newSentence = false
			}
		} else {
			maxWords, minWords := 50, 3
			if !firstSentense {
				maxWords, minWords = 100, 10
			}

			if nn := utf8.RuneCount([]byte(sentence)); nn >= maxWords {
				newSentence = true
			} else if nn < minWords {
				newSentence = false
			}
		}

		if finished || newSentence {
			stage.previousAssitant += sentence + " "
			// We utilize user ASR and AI responses as prompts for the subsequent ASR, given that this is
			// a chat-based scenario where the user converses with the AI, and the following audio should pertain to both user and AI text.
			stage.previousAsrText += " " + sentence

			isFirstSentence := firstSentense
			if firstSentense {
				firstSentense = false
				if robot.prefix != "" {
					sentence = fmt.Sprintf("%v %v", robot.prefix, sentence)
				}
				if v.onFirstResponse != nil {
					v.onFirstResponse(ctx)
				}
			}

			stage.ttsWorker.SubmitSegment(ctx, stage, NewAnswerSegment(func(segment *AnswerSegment) {
				segment.rid = rid
				segment.text = sentence
				segment.first = isFirstSentence
			}))
			sentence = ""
		}
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

	client := openai.NewClientWithConfig(aiConfig)
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
