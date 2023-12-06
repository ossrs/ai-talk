package main

import (
	"context"
	errors2 "errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
	"github.com/sashabaranov/go-openai"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var previousAsrText string

var previousUser, previousAssitant string
var histories []openai.ChatCompletionMessage

const AI_PROMPT = `
我希望你是一个儿童的词语接龙的助手。
我希望你做两个词的词语接龙。
我希望你不要用重复的词语。
我希望你回答比较简短，不超过50字。
我希望你重复我说的词，然后再接龙。
我希望你回答时，解释下词语的含义。
请记住，你讲的答案是给6岁小孩听得懂的。
请记住，你要做词语接龙。

例如：
我：苹果
你：苹果，果园
苹果，是一种水果，长在树上，是红色的。
果园，是一种地方，有很多树，有很多果子。
`
const AI_MODEL = openai.GPT4TurboPreview

var ttsWorker TTSWorker

type TTSStencense struct {
	rid      string
	uuid     string
	sentence string
	ttsFile  string
	ready    bool
	err      error
}

type TTSWorker struct {
	sentences []*TTSStencense
	lock      sync.Mutex
}

func (v *TTSWorker) Run(ctx context.Context) {
	go func() {
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}

			if len(v.sentences) == 0 {
				continue
			}

			if sentence := v.sentences[0]; !sentence.ready && sentence.err == nil {
				continue
			}

			func() {
				var sentence *TTSStencense
				func() {
					v.lock.Lock()
					defer v.lock.Unlock()

					sentence = v.sentences[0]
					v.sentences = v.sentences[1:]
				}()

				defer func() {
					if _, err := os.Stat(sentence.ttsFile); err == nil {
						logger.Tf(ctx, "remove %v", sentence.ttsFile)
						os.Remove(sentence.ttsFile)
					}
				}()

				if sentence.err != nil {
					logger.Ef(ctx, "TTS failed, err %+v", sentence.err)
				} else if sentence.ready {
					logger.Tf(ctx, "TTS ok, file %v", sentence.ttsFile)

					// Play the file.
					fmt.Fprintf(os.Stderr, fmt.Sprintf("Bot: %v\n", sentence.sentence))
					exec.CommandContext(ctx, "ffplay", "-autoexit", "-nodisp", sentence.ttsFile).Run()
				}
			}()
		}
	}()
}

func (v *TTSWorker) Add(ctx context.Context, s *TTSStencense) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.sentences = append(v.sentences, s)

	go func() {
		if err := func() error {
			var config openai.ClientConfig
			config = openai.DefaultConfig(os.Getenv("OPENAI_API_KEY"))
			if os.Getenv("OPENAI_PROXY") != "" {
				config.BaseURL = fmt.Sprintf("http://%v/v1", os.Getenv("OPENAI_PROXY"))
			}

			client := openai.NewClientWithConfig(config)
			resp, err := client.CreateSpeech(ctx, openai.CreateSpeechRequest{
				Model:          openai.TTSModel1,
				Input:          s.sentence,
				Voice:          openai.VoiceNova,
				ResponseFormat: openai.SpeechResponseFormatOpus,
			})
			if err != nil {
				return errors.Wrapf(err, "create speech")
			}
			defer resp.Close()

			out, err := os.Create(s.ttsFile)
			if err != nil {
				return errors.Errorf("Unable to create the file %v for writing", s.ttsFile)
			}
			defer out.Close()

			nn, err := io.Copy(out, resp)
			if err != nil {
				return errors.Errorf("Error writing the file")
			}

			s.ready = true
			logger.Tf(ctx, "File saved to %v, size: %v, %v", s.ttsFile, nn, s.sentence)
			return nil
		}(); err != nil {
			s.err = err
		}
	}()
}

func handleStream(ctx context.Context, rid string, stream *openai.ChatCompletionStream) error {
	var sentence string
	var finished bool
	firstSentense := true
	for !finished && ctx.Err() == nil {
		response, err := stream.Recv()
		finished = errors2.Is(err, io.EOF)
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
			if nn := strings.Count(sentence, " "); nn >= 10 {
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
				if !isEnglish(sentence) && utf8.RuneCount([]byte(sentence)) < 8 {
					sentence = fmt.Sprintf("我说的是：%v", sentence)
				}
			}

			s := &TTSStencense{
				rid:      rid,
				uuid:     uuid.NewString(),
				sentence: sentence,
			}
			s.ttsFile = fmt.Sprintf("/tmp/%v-sentence-%v-tts.opus", rid, s.uuid)
			sentence = ""

			ttsWorker.Add(ctx, s)
		}
	}

	return nil
}

func handleAudio(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	ctx = logger.WithContext(ctx)

	// For this request.
	rid := uuid.NewString()

	// We save the input audio to *.audio file, it can be aac or opus codec.
	inputFile := fmt.Sprintf("/tmp/%v-input.audio", rid)
	defer os.Remove(inputFile)
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

	outputFile := fmt.Sprintf("/tmp/%v-input.m4a", rid)
	defer os.Remove(outputFile)
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
			Language: "zh",
			Prompt:   previousAsrText,
		},
	)
	if err != nil {
		return errors.Wrapf(err, "transcription")
	}
	logger.Tf(ctx, "ASR ok prompt=<%v>, resp is <%v>", previousAsrText, resp.Text)
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

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: AI_PROMPT},
	}
	messages = append(messages, histories...)
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: previousAsrText,
	})
	stream, err := client.CreateChatCompletionStream(
		ctx, openai.ChatCompletionRequest{
			Model:       AI_MODEL,
			Messages:    messages,
			Stream:      true,
			Temperature: 0.9,
		},
	)
	if err != nil {
		return errors.Wrapf(err, "create chat")
	}

	// Never wait for any response.
	go func() {
		defer stream.Close()
		if err := handleStream(ctx, rid, stream); err != nil {
			logger.Ef(ctx, "Handle stream failed, err %+v", err)
		}
	}()

	// Response the request UUID and pulling the response.
	ohttp.WriteData(ctx, w, r, struct {
		UUID string `json:"uuid"`
	}{
		UUID: rid,
	})
	return nil
}

func doMain(ctx context.Context) error {
	if err := godotenv.Overload(); err != nil {
		return errors.Wrapf(err, "load env")
	}

	http.HandleFunc("/api/ai-talk/upload/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleAudio(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle audio failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	ttsWorker.Run(ctx)

	listen := ":3001"
	fmt.Fprintf(os.Stderr, fmt.Sprintf("Listen at %v\n", listen))
	logger.Tf(ctx, "Listen at %v", listen)
	return http.ListenAndServe(listen, nil)
}

func main() {
	ctx := context.Background()
	if err := doMain(ctx); err != nil {
		logger.E(ctx, "Main error: %v", err)
		os.Exit(-1)
	}
}
