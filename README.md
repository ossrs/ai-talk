# AI Talk

AI-Talk allows you to talk with OpenAI GPT.

<details>
<summary>General Assistant</summary>

Please setup the envirionments:
```
AIT_SYSTEM_PROMPT='You are a helpful assistant.'
```

https://github.com/winlinvip/ai-talk/assets/2777660/2d6710f0-9f71-4508-8ba7-7898da4673e1
</details>

<details>
<summary>Spoken English Coach</summary>

Please setup the envirionments:
```
AIT_SYSTEM_PROMPT='I want you to act as a spoken English teacher and improver. I will speak to you in English and you will reply to me in English to practice my spoken English. I want you to  I want you to strictly correct my grammar mistakes, typos, and factual errors. I want you to ask me a question in your reply. Now let us start practicing, you could ask me a question first. Remember, I want you to strictly correct my grammar mistakes, typos, and factual errors.'
```
    
https://github.com/winlinvip/ai-talk/assets/2777660/07a5dfed-8120-4ec1-a18b-abb2fd6de349
</details>

<details>
<summary>Simultaneous English Translation</summary>

Please setup the envirionments:
```
AIT_SYSTEM_PROMPT='Translate to simple and easy to understand english. Never answer questions but only translate text to English.'
```

https://github.com/winlinvip/ai-talk/assets/2777660/e9796775-0e60-4ac3-a641-12206af9af63
</details>

<details>
<summary>Chinese Words Solitaire Game</summary>

Please setup the envirionments:
```
AIT_ASR_LANGUAGE=zh
AIT_SYSTEM_PROMPT='我希望你是一个儿童的词语接龙的助手。我希望你做两个词的词语接龙。我希望你不要用重复的词语。我希望你重复我说的词，然后再接龙。我希望你回答时，解释下词语的含义。请记住，你讲的答案是给6岁小孩听得懂的。请记住，你要做词语接龙。例如：我：苹果。你：苹果，果园。苹果，是一种水果，长在树上，是红色的。果园，是一种地方，有很多树，有很多果子。'
```

https://github.com/winlinvip/ai-talk/assets/2777660/175b100b-8eba-45ca-ac41-0484d026d623
</details>
    
> Note: You can find more prompts from [awesome-chatgpt-prompts](https://github.com/f/awesome-chatgpt-prompts).

## Usage

To run in docker:

```bash
docker run --rm -it -p 80:3000 -p 443:3443 \
    -e OPENAI_API_KEY=sk-xxx -e OPENAI_PROXY=api.openai.com \
    -e AIT_SYSTEM_PROMPT="You are a helpful assistant." \
    -e AIT_CHAT_MODEL="gpt-4-1106-preview" \
    ossrs/ai-talk
```

> Note: Setup the `OPENAI_PROXY` if you are not able to access the API directly.

> Note: Please use `registry.cn-hangzhou.aliyuncs.com/ossrs/ai-talk` in China.

Then you can access by http://localhost and happy to talk with AI.

Then you can access by https://your-server-ip from your mobile browser.

> Note: The HTTPS certificate is self-signed, you need to accept it in your mobile browser.

## Environment Variables

Necessary environment variables that you must configure:

* `OPENAI_API_KEY`: The OpenAI API key, get from [https://platform.openai.com/api-keys](https://platform.openai.com/api-keys).

Optionally, you might need to set the following environment variables:

* `OPENAI_PROXY`: The OpenAI API proxy, default to `api.openai.com`, which directly access OpenAI API without proxy.
* `AIT_SYSTEM_PROMPT`: The system prompt, default to `You are a helpful assistant.`.
  * To make sure AI response limit words to avoid long audio, we always append `Keep your reply neat, limiting the reply to ${AIT_REPLY_LIMIT} words.` to system prompt.
  * You can set `AIT_REPLY_LIMIT` to limit the words of AI response, default to `50`.
  * Please use `AIT_EXTRA_ROBOTS` to set the number of extra robots, default to `0`.
* `AIT_CHAT_MODEL`: The AI model, default to `gpt-3.5-turbo-1106` which is good enough. See [link](https://platform.openai.com/docs/models).
  * `gpt-4-1106-preview`: The latest GPT-4 model with improved instruction following, JSON mode, reproducible outputs, parallel function calling, and more.
  * `gpt-3.5-turbo-1106`: The latest GPT-3.5 Turbo model with improved instruction following, JSON mode, reproducible outputs, parallel function calling, and more.
  * `gpt-3.5-turbo`: Our most capable and cost effective model in the GPT-3.5 family.
* `AIT_ASR_LANGUAGE`: The language for Whisper ASR, default to `en`, see [ISO-639-1](https://en.wikipedia.org/wiki/List_of_ISO_639-1_codes), bellow are some examples:
  * `en`: English
  * `zh`: Chinese
  * `fr`: French 
  * `de`: German
  * `it`: Italian
  * `ja`: Japanese
  * `ko`: Korean
  * `pt`: Portuguese
  * `ru`: Russian
  * `es`: Spanish
* `AIT_ASR_MODEL`: The model for Whisper ASR, default to `whisper-1`. See [link](https://platform.openai.com/docs/api-reference/audio/createTranscription).
* `AIT_TTS_VOICE`: The void for OpenAI TTS, default to `nova`. Supported voices are `alloy`, `echo`, `fable`, `onyx`, `nova`, and `shimmer`. See [link](https://platform.openai.com/docs/api-reference/audio/createSpeech).
* `AIT_TTS_MODEL`: The model to use for OpenAI TTS, default to `tts-1`. See [link](https://platform.openai.com/docs/api-reference/audio/createSpeech)

Optionally, additional robots can be incorporated into various environments:

* `AIT_EXTRA_ROBOTS`: The number of extra robots, default to `0`.
* `AIT_ROBOT_0_LABEL`: The label for extra robot `#0`, for example, `English Spoken Coach`.
* `AIT_ROBOT_0_PROMPT`: The prompt for extra robot `#0`, for example, `I want you to act as a spoken English teacher and improver.`.
* `AIT_ROBOT_0_ASR_LANGUAGE`: The language for extra robot `#0`, see `AIT_ASR_LANGUAGE`, default to `en`.
* `AIT_ROBOT_0_PREFIX`: (Optional) The prefix for the first sentence for extra robot `#0`, see `AIT_REPLY_PREFIX`.

Less frequently used optional environment variables:

* `AIT_HTTP_LISTEN`: The HTTP listen address, default to `:3000`, please use `-p 80:3000` to map to a different port.
* `AIT_HTTPS_LISTEN`: The HTTPS listen address, default to `:3443`, please use `-p 443:3443` to map to a different port.
* `AIT_PROXY_STATIC`: Whether proxy to static files, default to `false`.
* `AIT_REPLY_PREFIX`: If AI reply is very short for TTS not good, prefix with this text, default is not set.
* `AIT_MAX_TOKENS`: The max tokens, default to `1024`.
* `AIT_TEMPERATURE`: The temperature, default to `0.9`.
* `AIT_KEEP_FILES`: Whether keep audio files, default to `false`.
* `AIT_REPLY_LIMIT`: The AI reply limit words, default to `50`.
* `AIT_CHAT_WINDOW`: The AI chat window to store historical messages, default to `5`.
* `AIT_DEFAULT_ROBOT`: Whether enable the default robot, prompt is `AIT_SYSTEM_PROMPT`, default to `true`.
* `AIT_STAGE_TIMEOUT`: The timeout in seconds for each stage, default to `300`.

## HTTPS Certificate

You can buy and download HTTPS certificate, then mount to docker by:

```bash
docker run -v /path/to/domain.crt:/g/server.crt -v /path/to/domain.key:/g/server.key
```

Please make sure the file `/path/to/domain.crt` and `/path/to/domain.key` exists.

## Changelog

The changelog:

* v1.0:
  * Support mobile and PC browser. v1.0.0
  * Request microphone permission when starting. v1.0.2
  * Support single assistant to talk with. v1.0.3
  * Dispose recorder and stream when record done. v1.0.4
  * Setup the website title to AI Talk whatever. v1.0.6
  * Limit the response words to 50 words by default. v1.0.10
  * Support multiple assistants and select before start. v1.0.11
  * Setup model and robots by environment variables. [v1.0.12](https://github.com/winlinvip/ai-talk/releases/tag/v1.0.12)
  * Fix Android browser select button label issue. v1.0.13
  * Add micWorking and attention state to avoid data loss. v1.0.13

Winlin, 2023.12
