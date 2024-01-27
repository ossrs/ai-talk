package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"
	"github.com/tencentcloud/tencentcloud-speech-sdk-go/asr"
	"github.com/tencentcloud/tencentcloud-speech-sdk-go/common"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

var tencentAIConfig tencentConfig

type tencentConfig struct {
	AppID, SecretID, SecretKey string
}

func tencentInit(ctx context.Context) {
	tencentAIConfig.AppID = os.Getenv("TENCENT_SPEECH_APPID")
	tencentAIConfig.SecretID = os.Getenv("TENCENT_SECRET_ID")
	tencentAIConfig.SecretKey = os.Getenv("TENCENT_SECRET_KEY")
	logger.Tf(ctx, "Tencent config, speech appid=%v, key=%v, secret=%vB",
		tencentAIConfig.AppID, tencentAIConfig.SecretID, len(tencentAIConfig.SecretKey))
}

type tencentASRService struct {
}

func NewTencentASRService() ASRService {
	return &tencentASRService{}
}

func (v *tencentASRService) RequestASR(ctx context.Context, inputFile, language, prompt string, onBeforeRequest func()) (*ASRResult, error) {
	outputFile := fmt.Sprintf("%v.wav", inputFile)

	// Transcode input audio in opus or aac, to aac in m4a format.
	if os.Getenv("AIT_KEEP_FILES") != "true" {
		defer os.Remove(outputFile)
	}
	if true {
		err := exec.CommandContext(ctx, "ffmpeg",
			"-i", inputFile,
			"-vn", "-c:a", "pcm_s16le", "-ac", "1", "-ar", "16000",
			outputFile,
		).Run()

		if err != nil {
			return nil, errors.Errorf("Error converting the file")
		}
		logger.Tf(ctx, "Convert audio %v to %v ok", inputFile, outputFile)
	}

	duration, _, err := ffprobeAudio(ctx, outputFile)
	if err != nil {
		return nil, errors.Wrapf(err, "ffprobe")
	}

	if onBeforeRequest != nil {
		onBeforeRequest()
	}

	// Request ASR.
	EngineModelType := "16k_zh"
	if language == "en" {
		EngineModelType = "16k_en"
	}

	recognizer := asr.NewFlashRecognizer(
		tencentAIConfig.AppID, common.NewCredential(tencentAIConfig.SecretID, tencentAIConfig.SecretKey),
	)

	data, err := ioutil.ReadFile(outputFile)
	if err != nil {
		return nil, errors.Wrapf(err, "read wav file %v", outputFile)
	}

	req := new(asr.FlashRecognitionRequest)
	req.EngineType = EngineModelType
	req.VoiceFormat = "wav"
	req.SpeakerDiarization = 0
	req.FilterDirty = 0
	req.FilterModal = 0
	req.FilterPunc = 0
	req.ConvertNumMode = 1
	req.FirstChannelOnly = 1
	req.WordInfo = 0

	resp, err := recognizer.Recognize(req, data)
	if err != nil {
		return nil, errors.Wrapf(err, "recognize error")
	}

	var sb strings.Builder
	for _, channelResult := range resp.FlashResult {
		sb.WriteString(channelResult.Text)
		sb.WriteString(" ")
	}

	return &ASRResult{Text: strings.TrimSpace(sb.String()), Duration: time.Duration(duration * float64(time.Second))}, nil
}

type tencentTTSService struct {
}

func NewTencentTTSService() TTSService {
	return &tencentTTSService{}
}

func (v *tencentTTSService) RequestTTS(ctx context.Context, buildFilepath func(ext string) string, text string) error {
	ttsFile := buildFilepath("wav")
	appID, err := strconv.ParseInt(tencentAIConfig.AppID, 10, 64)
	if err != nil {
		return errors.Wrapf(err, "parse appid %v", tencentAIConfig.AppID)
	}

	requestData := map[string]interface{}{
		"Action":          "TextToStreamAudio",
		"AppId":           int(appID), // replace with your AppId
		"Codec":           "pcm",
		"Expired":         time.Now().Unix() + 3600,
		"ModelType":       0,
		"PrimaryLanguage": 1,
		"ProjectId":       0,
		"SampleRate":      16000,
		"SecretId":        tencentAIConfig.SecretID, // replace with your SecretId
		"SessionId":       "12345678",
		"Speed":           0,
		"Text":            text,
		"Timestamp":       time.Now().Unix(),
		"VoiceType":       1009,
		"Volume":          5,
	}

	url := "https://tts.cloud.tencent.com/stream"

	jsonData, err := json.Marshal(requestData)
	if err != nil {
		return errors.Wrapf(err, "marshal json")
	}

	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return errors.Wrapf(err, "create request")
	}
	req.Header.Set("Content-Type", "application/json")

	signature := v.authGenerateSign(tencentAIConfig.SecretKey, requestData) // replace with your SecretKey
	req.Header.Set("Authorization", signature)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrapf(err, "request tts")
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrapf(err, "read body")
	}
	if strings.Contains(string(body), "Error") {
		return errors.Errorf("tts error: %v", string(body))
	}

	out, err := os.Create(ttsFile)
	if err != nil {
		return errors.Wrapf(err, "create file")
	}
	defer out.Close()

	// Convert to s16le depth.
	data := make([]int, len(body)/2)
	for i := 0; i < len(body); i += 2 {
		data[i/2] = int(binary.LittleEndian.Uint16(body[i : i+2]))
	}

	enc := wav.NewEncoder(out, 16000, 16, 1, 1)
	defer enc.Close()

	ib := &audio.IntBuffer{
		Data: data, SourceBitDepth: 16,
		Format: &audio.Format{NumChannels: 1, SampleRate: 16000},
	}
	if err := enc.Write(ib); err != nil {
		return errors.Wrapf(err, "copy body")
	}
	return nil
}

func (v *tencentTTSService) authGenerateSign(secretKey string, requestData map[string]interface{}) string {
	url := "tts.cloud.tencent.com/stream"
	signStr := "POST" + url + "?"

	keys := []string{}
	for k := range requestData {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		signStr = fmt.Sprintf("%s%s=%v&", signStr, k, requestData[k])
	}
	signStr = signStr[:len(signStr)-1]

	h := hmac.New(sha1.New, []byte(secretKey))
	h.Write([]byte(signStr))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
