package main

import (
	"context"
	errors2 "errors"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"
	openai "github.com/sashabaranov/go-openai"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
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

func (v *TTSWorker) Add(ctx context.Context, sentence string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	s := TTSStencense{
		sentence: sentence,
		ttsFile:  fmt.Sprintf("/tmp/tts-%v-%v-%v.opus", os.Getpid(), time.Now().UnixNano(), rand.Int()),
	}
	v.sentences = append(v.sentences, &s)

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
				Input:          sentence,
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

func handleStream(ctx context.Context, stream *openai.ChatCompletionStream) error {
	var sentence string
	var finished bool
	for !finished && ctx.Err() == nil {
		response, err := stream.Recv()
		finished = errors2.Is(err, io.EOF)
		if err != nil && !finished {
			return errors.Wrapf(err, "recv chat")
		}

		var dc string
		if len(response.Choices) > 0 {
			choice := response.Choices[0]
			dc = choice.Delta.Content
		}

		newSentence := false
		if dc != "" {
			filteredStencese := strings.ReplaceAll(dc, "\n\n", "\n")
			filteredStencese = strings.ReplaceAll(filteredStencese, "\n", " ")
			sentence += filteredStencese

			if dc == "," || dc == "." || dc == "?" || dc == "!" || strings.Contains(dc, "\n") {
				newSentence = true
			}
			if dc == "。" || dc == "？" || dc == "！" {
				newSentence = true
			}
			if len(sentence) <= 3 || sentence == "" {
				newSentence = false
			}
		}

		if sentence != "" && (finished || newSentence) {
			previousAssitant += sentence + " "
			ttsWorker.Add(ctx, sentence)
			sentence = ""
		}
	}

	return nil
}

func handleAudio(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	ctx = logger.WithContext(ctx)

	inputFile := fmt.Sprintf("/tmp/audio-%v-%v-%v.aac", os.Getpid(), time.Now().UnixNano(), rand.Int())
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
	defer os.Remove(inputFile)

	// aac to m4a
	// opus to ogg
	outputFile := fmt.Sprintf("/tmp/audio-%v-%v-%v.m4a", os.Getpid(), time.Now().UnixNano(), rand.Int())
	if true {
		err := exec.CommandContext(ctx, "ffmpeg",
			"-i", inputFile, "-c", "copy", "-y", outputFile,
		).Run()
		if err != nil {
			return errors.Errorf("Error converting the file")
		}
		logger.Tf(ctx, "Convert to ogg %v ok", outputFile)
		defer os.Remove(outputFile)
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
	defer stream.Close()

	if err := handleStream(ctx, stream); err != nil {
		return errors.Wrapf(err, "handle stream")
	}

	// Wait for sentences to be consumed.
	consumed, cancel := context.WithCancel(ctx)

	go func() {
		for ctx.Err() == nil {
			if len(ttsWorker.sentences) == 0 {
				logger.Tf(ctx, "All sentences consumed")
				cancel()
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-ctx.Done():
	case <-consumed.Done():
	}

	// OK, process next message.
	w.Write([]byte(asrText))
	return nil
}

func doMain(ctx context.Context) error {
	if err := godotenv.Overload(); err != nil {
		return errors.Wrapf(err, "load env")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleAudio(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle audio failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	ttsWorker.Run(ctx)

	listen := ":3001"
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
