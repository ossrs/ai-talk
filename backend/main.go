package main

import (
	"context"
	errors_std "errors"
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

// You can overwrite it by env AI_SYSTEM_PROMPT.
const AI_SYSTEM_PROMPT = `
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

// You can overwrite it by env AI_MODEL.
const AI_MODEL = openai.GPT4TurboPreview

// Disable padding by set env AI_NO_PADDING=1.
const FirstSentencePaddingLength = 8
const FirstSentencePaddingText = "我说的是："

// You can overwrite it by env AI_MAX_TOKENS.
const AI_MAX_TOKENS = 1024

// You can overwrite it by env AI_TEMPERATURE.
const AI_TEMPERATURE = 0.9

var ttsWorker TTSWorker
var previousAsrText string
var previousUser, previousAssitant string
var histories []openai.ChatCompletionMessage
var aiConfig openai.ClientConfig
var workDir string

type TTSStencense struct {
	// Request UUID.
	rid string
	// Sentence UUID.
	uuid string
	// Sentence text.
	sentence string
	// The TTS file path.
	ttsFile string
	// Whether TTS is done, ready to play.
	ready bool
	// Whether TTS is error, sentence failed.
	err error
	// Whether dummy sentence, to identify the request is alive.
	dummy bool
	// Signal to remove the TTS file immediately.
	removeSignal chan bool
}

type TTSWorker struct {
	sentences []*TTSStencense
	lock      sync.Mutex
	wg        sync.WaitGroup
}

func (v *TTSWorker) Close() error {
	v.wg.Wait()
	return nil
}

func (v *TTSWorker) QueryTTS(requestUUID, uuid string) *TTSStencense {
	v.lock.Lock()
	defer v.lock.Unlock()

	for _, s := range v.sentences {
		if s.rid == requestUUID && s.uuid == uuid {
			return s
		}
	}

	return nil
}

func (v *TTSWorker) QueryReady(ctx context.Context, requestUUID string) *TTSStencense {
	for ctx.Err() == nil {
		if s := v.Query(requestUUID); s == nil {
			return nil
		} else if !s.dummy && (s.ready || s.err != nil) {
			return s
		}

		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}

	return nil
}

func (v *TTSWorker) Query(requestUUID string) *TTSStencense {
	v.lock.Lock()
	defer v.lock.Unlock()

	for _, s := range v.sentences {
		if s.rid == requestUUID {
			return s
		}
	}

	return nil
}

func (v *TTSWorker) Remove(uuid string) {
	v.lock.Lock()
	defer v.lock.Unlock()

	for i, s := range v.sentences {
		if s.uuid == uuid {
			v.sentences = append(v.sentences[:i], v.sentences[i+1:]...)
			return
		}
	}
}

func (v *TTSWorker) Add(ctx context.Context, s *TTSStencense) {
	// Append the sentence to queue.
	func() {
		v.lock.Lock()
		defer v.lock.Unlock()

		v.sentences = append(v.sentences, s)
	}()

	// Now that we have a real sentence, we should remove the dummy sentence.
	if dummy := v.Query(s.rid); dummy != nil && dummy.dummy && dummy.uuid != s.uuid {
		v.Remove(dummy.uuid)
	}

	// Start a goroutine to do TTS task.
	v.wg.Add(1)
	go func() {
		defer v.wg.Done()

		if err := func() error {
			client := openai.NewClientWithConfig(aiConfig)
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

		// Start a goroutine to remove the sentence.
		v.wg.Add(1)
		go func() {
			defer v.wg.Done()

			select {
			case <-ctx.Done():
			case <-time.After(300 * time.Second):
			case <-s.removeSignal:
			}

			logger.Tf(ctx, "Remove %v %v", s.uuid, s.ttsFile)

			ttsWorker.Remove(s.uuid)

			if s.ttsFile != "" {
				if _, err := os.Stat(s.ttsFile); err == nil {
					os.Remove(s.ttsFile)
				}
			}
		}()
	}()
}

func handleStream(ctx context.Context, rid string, stream *openai.ChatCompletionStream) error {
	var sentence string
	var finished bool
	firstSentense := true
	for !finished && ctx.Err() == nil {
		response, err := stream.Recv()
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
				if os.Getenv("AI_NO_PADDING") != "1" &&
					!isEnglish(sentence) &&
					utf8.RuneCount([]byte(sentence)) < FirstSentencePaddingLength {
					sentence = fmt.Sprintf("%v%v", FirstSentencePaddingText, sentence)
				}
			}

			s := &TTSStencense{
				rid:          rid,
				uuid:         uuid.NewString(),
				sentence:     sentence,
				removeSignal: make(chan bool, 1),
			}
			s.ttsFile = path.Join(workDir, fmt.Sprintf("%v-sentence-%v-tts.opus", rid, s.uuid))
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
	inputFile := path.Join(workDir, fmt.Sprintf("%v-input.audio", rid))
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

	outputFile := path.Join(workDir, fmt.Sprintf("%v-input.m4a", rid))
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

	system := AI_SYSTEM_PROMPT
	if os.Getenv("AI_SYSTEM_PROMPT") != "" {
		system = os.Getenv("AI_SYSTEM_PROMPT")
	}
	logger.Tf(ctx, "AI system prompt(AI_SYSTEM_PROMPT): %v", system)
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: system},
	}

	messages = append(messages, histories...)
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: previousAsrText,
	})

	model := AI_MODEL
	if os.Getenv("AI_MODEL") != "" {
		model = os.Getenv("AI_MODEL")
	}

	maxTokens := AI_MAX_TOKENS
	if os.Getenv("AI_MAX_TOKENS") != "" {
		if v, err := strconv.ParseInt(os.Getenv("AI_MAX_TOKENS"), 10, 64); err != nil {
			return errors.Wrapf(err, "parse AI_MAX_TOKENS")
		} else {
			maxTokens = int(v)
		}
	}

	temperature := AI_TEMPERATURE
	if os.Getenv("AI_TEMPERATURE") != "" {
		if v, err := strconv.ParseFloat(os.Getenv("AI_TEMPERATURE"), 64); err != nil {
			return errors.Wrapf(err, "parse AI_TEMPERATURE")
		} else {
			temperature = v
		}
	}
	logger.Tf(ctx, "AI model(AI_MODEL): %v, max tokens(AI_MAX_TOKENS): %v, temperature(AI_TEMPERATURE): %v",
		model, maxTokens, temperature)

	stream, err := client.CreateChatCompletionStream(
		ctx, openai.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			Stream:      true,
			Temperature: float32(temperature),
			MaxTokens:   maxTokens,
		},
	)
	if err != nil {
		return errors.Wrapf(err, "create chat")
	}

	// Insert a dummy sentence to identify the request is alive.
	ttsWorker.Add(ctx, &TTSStencense{
		rid:   rid,
		uuid:  uuid.NewString(),
		dummy: true,
	})

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
		ASR  string `json:"asr"`
	}{
		UUID: rid,
		ASR:  asrText,
	})
	return nil
}

func handleQuestion(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rid := r.URL.Query().Get("rid")
	if rid == "" {
		return errors.Errorf("empty rid")
	}

	sentence := ttsWorker.QueryReady(ctx, rid)
	if sentence == nil {
		ohttp.WriteData(ctx, w, r, struct {
			UUID string `json:"uuid"`
		}{})
		return nil
	}

	ohttp.WriteData(ctx, w, r, struct {
		Processing bool   `json:"processing"`
		UUID       string `json:"uuid"`
		TTS        string `json:"tts"`
	}{
		Processing: sentence.dummy || !sentence.ready,
		UUID:       sentence.uuid,
		TTS:        sentence.sentence,
	})
	return nil
}

func handleTTS(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	rid := r.URL.Query().Get("rid")
	if rid == "" {
		return errors.Errorf("empty rid")
	}

	uuid := r.URL.Query().Get("uuid")
	if uuid == "" {
		return errors.Errorf("empty uuid")
	}

	sentence := ttsWorker.QueryTTS(rid, uuid)
	if sentence == nil {
		return errors.Errorf("no sentence for %v %v", rid, uuid)
	}

	fmt.Fprintf(os.Stderr, "Bot: %v\n", sentence.sentence)

	// Read the ttsFile and response it as opus audio.
	w.Header().Set("Content-Type", "audio/opus")
	http.ServeFile(w, r, sentence.ttsFile)

	return nil
}

func handleRemove(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	rid := r.URL.Query().Get("rid")
	if rid == "" {
		return errors.Errorf("empty rid")
	}

	uuid := r.URL.Query().Get("uuid")
	if uuid == "" {
		return errors.Errorf("empty uuid")
	}

	sentence := ttsWorker.QueryTTS(rid, uuid)
	if sentence == nil {
		return errors.Errorf("no sentence for %v %v", rid, uuid)
	}

	// Remove it.
	ttsWorker.Remove(uuid)

	select {
	case <-ctx.Done():
	case sentence.removeSignal <- true:
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

	// Load env vriables from file.
	if _, err := os.Stat("../.env"); err != nil {
		return errors.Wrapf(err, "not found .env")
	}
	if err := godotenv.Overload("../.env"); err != nil {
		return errors.Wrapf(err, "load env")
	}

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
	http.HandleFunc("/api/ai-talk/upload/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleAudio(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle audio failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/api/ai-talk/question/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleQuestion(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle question failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/api/ai-talk/tts/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleTTS(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle tts failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/api/ai-talk/remove/", func(w http.ResponseWriter, r *http.Request) {
		if err := handleRemove(ctx, w, r); err != nil {
			logger.Ef(ctx, "Handle remove failed, err %+v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Setup the work dir.
	if pwd, err := os.Getwd(); err != nil {
		return errors.Wrapf(err, "getwd")
	} else {
		workDir = pwd
	}

	// Start HTTP server.
	listen := ":3001"
	fmt.Fprintf(os.Stderr, fmt.Sprintf("Listen at %v, workDir=%v\n", listen, workDir))
	logger.Tf(ctx, "Listen at %v", listen)
	server := &http.Server{Addr: listen, Handler: nil}

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
