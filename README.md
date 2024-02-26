# AI Talk

[![](https://badgen.net/discord/members/q29TwKwC2C)](https://discord.gg/q29TwKwC2C)

AI-Talk allows you to talk with OpenAI GPT.

<details>
<summary>General Assistant</summary>

Please setup the envirionments:
```
AIT_SYSTEM_PROMPT='You are a helpful assistant.'
```

https://github.com/ossrs/ai-talk/assets/2777660/57599a76-37d7-4b12-99be-cb3b96e1742a
</details>

<details>
<summary>Spoken English Coach</summary>

Please setup the envirionments:
```
AIT_SYSTEM_PROMPT='I want you to act as a spoken English teacher and improver. I will speak to you in English and you will reply to me in English to practice my spoken English. I want you to  I want you to strictly correct my grammar mistakes, typos, and factual errors. I want you to ask me a question in your reply. Now let us start practicing, you could ask me a question first. Remember, I want you to strictly correct my grammar mistakes, typos, and factual errors.'
```
    
https://github.com/ossrs/ai-talk/assets/2777660/b797e8a4-7656-410b-8250-e0abaa4b037d
</details>

<details>
<summary>Simultaneous English Translation</summary>

Please setup the envirionments:
```
AIT_SYSTEM_PROMPT='Translate to simple and easy to understand english. Never answer questions but only translate text to English.'
```

https://github.com/ossrs/ai-talk/assets/2777660/a33e56c2-0f88-499f-8e4a-7b537a1e9ba9
</details>

<details>
<summary>Chinese Words Solitaire Game</summary>

Please setup the envirionments:
```
AIT_ASR_LANGUAGE=zh
AIT_SYSTEM_PROMPT='我希望你是一个儿童的词语接龙的助手。我希望你做两个词的词语接龙。我希望你不要用重复的词语。我希望你重复我说的词，然后再接龙。我希望你回答时，解释下词语的含义。请记住，你讲的答案是给6岁小孩听得懂的。请记住，你要做词语接龙。例如：我：苹果。你：苹果，果园。苹果，是一种水果，长在树上，是红色的。果园，是一种地方，有很多树，有很多果子。'
```

https://github.com/ossrs/ai-talk/assets/2777660/bb350595-23a6-47df-a050-c931699ac7e3
</details>
    
> Note: You can find more prompts from [awesome-chatgpt-prompts](https://github.com/f/awesome-chatgpt-prompts).

## Usage

To run in docker:

```bash
docker run --rm -it -p 80:3000 -p 443:3443 \
    -e OPENAI_API_KEY=sk-xxx -e OPENAI_PROXY=https://api.openai.com/v1 \
    -e AIT_SYSTEM_PROMPT="You are a helpful assistant." \
    -e AIT_CHAT_MODEL="gpt-4-1106-preview" \
    ossrs/ai-talk:v1
```

> Note: Setup the `OPENAI_PROXY` if you are not able to access the API directly.

> Note: Please use `registry.cn-hangzhou.aliyuncs.com/ossrs/ai-talk:v1` in China.

Then you can access by http://localhost and happy to talk with AI.

Then you can access by https://your-server-ip from your mobile browser.

> Note: The HTTPS certificate is self-signed, you need to accept it in your mobile browser.

## aaPanel or BaoTa

You can use [aaPanel](https://www.aapanel.com/) or [BaoTa](https://www.bt.cn/) to deploy AI-Talk.

First, run AI Talk in docker, only listen at HTTP:

```bash
docker run --rm -it -p 3000:3000 \
    -e OPENAI_API_KEY=sk-xxx -e OPENAI_PROXY=https://api.openai.com/v1 \
    -e AIT_SYSTEM_PROMPT="You are a helpful assistant." \
    -e AIT_CHAT_MODEL="gpt-4-1106-preview" \
    ossrs/ai-talk:v1
```

Then, create a website in aaPanel or BaoTa, with nginx config as bellow:

```nginx
    location / {
      proxy_pass http://127.0.0.1:3000$request_uri;
    }
    if ($server_port !~ 443){
        rewrite ^(/.*)$ https://$host$1 permanent;
    }
```

Next, setup HTTPS by aaPanel or BaoTa.

Finally, access the website by https://your-domain-name.

## Environment Variables

Necessary environment variables that you must configure:

* `OPENAI_API_KEY`: The OpenAI API key, get from [https://platform.openai.com/api-keys](https://platform.openai.com/api-keys).

Optionally, you might need to set the following environment variables:

* `OPENAI_PROXY`: The OpenAI API proxy, default to `https://api.openai.com/v1`, which directly access OpenAI API without proxy.
  * `OPENAI_ORGANIZATION`: The OpenAI organization.
  * `ASR_OPENAI_API_KEY`: The OpenAI API key for ASR, default to `OPENAI_API_KEY`. 
  * `ASR_OPENAI_PROXY`: The OpenAI API proxy for ASR, default to `OPENAI_PROXY`.
  * `CHAT_OPENAI_API_KEY`: The OpenAI API key for chat, default to `OPENAI_API_KEY`.
  * `CHAT_OPENAI_PROXY`: The OpenAI API proxy for chat, default to `OPENAI_PROXY`.
  * `TTS_OPENAI_API_KEY`: The OpenAI API key for TTS, default to `OPENAI_API_KEY`.
  * `TTS_OPENAI_PROXY`: The OpenAI API proxy for TTS, default to `OPENAI_PROXY`.
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

* `AIT_ROBOT_0_ID`: The id for extra robot `#0`, for example, `english-spoken-coach`.
* `AIT_ROBOT_0_LABEL`: The label for extra robot `#0`, for example, `English Spoken Coach`.
* `AIT_ROBOT_0_PROMPT`: The prompt for extra robot `#0`, for example, `I want you to act as a spoken English teacher and improver.`.
* `AIT_ROBOT_0_ASR_LANGUAGE`: **(Optional)** The language for extra robot `#0`, default to `AIT_ASR_LANGUAGE`.
* `AIT_ROBOT_0_REPLY_PREFIX`: **(Optional)** The prefix for the first sentence for extra robot `#0`, default to `AIT_REPLY_PREFIX`.
* `AIT_ROBOT_0_REPLY_LIMIT`: **(Optional)** The limit words for extra robot `#0`, default to `AIT_REPLY_LIMIT`.
* `AIT_ROBOT_0_CHAT_MODEL`: **(Optional)** The AI chat model for extra robot `#0`, default to `AIT_CHAT_MODEL`.
* `AIT_ROBOT_0_CHAT_WINDOW`: **(Optional)** The AI chat window for extra robot `#0`, default to `AIT_CHAT_WINDOW`.

Less frequently used optional environment variables:

* `AIT_HTTP_LISTEN`: The HTTP listen address, default to `:3000`, please use `-p 80:3000` to map to a different port.
* `AIT_HTTPS_LISTEN`: The HTTPS listen address, default to `:3443`, please use `-p 443:3443` to map to a different port.
* `AIT_PROXY_STATIC`: Whether proxy to static files, default to `false`.
* `AIT_REPLY_PREFIX`: If AI reply is very short for TTS not good, prefix with this text, default is not set.
* `AIT_MAX_TOKENS`: The max tokens, default to `1024`.
* `AIT_TEMPERATURE`: The temperature, default to `0.9`.
* `AIT_KEEP_FILES`: Whether keep audio files, default to `false`.
* `AIT_REPLY_LIMIT`: The AI reply limit words, default to `30`.
* `AIT_CHAT_WINDOW`: The AI chat window to store historical messages, default to `5`.
* `AIT_DEFAULT_ROBOT`: Whether enable the default robot, prompt is `AIT_SYSTEM_PROMPT`, default to `true`.
* `AIT_STAGE_TIMEOUT`: The timeout in seconds for each stage, default to `300`.

## HTTPS Certificate

You can buy and download HTTPS certificate, then mount to docker by `-v` as bellow:

```bash
docker run \
    -v /path/to/domain.crt:/g/server.crt -v /path/to/domain.key:/g/server.key \
    ossrs/ai-talk:v1
```

Please make sure the file `/path/to/domain.crt` and `/path/to/domain.key` exists.

## Changelog

The changelog:

* Support mobile and PC browser. v1.0.0
* Request microphone permission when starting. v1.0.2
* Support single assistant to talk with. v1.0.3
* Dispose recorder and stream when record done. v1.0.4
* Setup the website title to AI Talk whatever. v1.0.6
* Limit the response words to 50 words by default. v1.0.10
* Support multiple assistants and select before start. v1.0.11
* Setup model and robots by environment variables. [v1.0.12](https://github.com/ossrs/ai-talk/releases/tag/v1.0.12)
* Fix Android browser select button label issue. v1.0.13
* Add micWorking and attention state to avoid data loss. v1.0.14
* Refine the UI for mobile browser. v1.0.15
* Fast require microphone permission. v1.0.16
* Save user select robot to local storage. [v1.0.17](https://github.com/ossrs/ai-talk/releases/tag/v1.0.17)
* Refine the UI and history message style. v1.0.18
* Limit to 30 words and config in robot. v1.0.19
* Use css to draw a microphone icon. [v1.0.20](https://github.com/ossrs/ai-talk/releases/tag/v1.0.20)
* Refine the data loss before and after record. v1.0.21
* Add text tips label to use microphone. v1.0.22
* Refine the startup waiting UI. [v1.0.23](https://github.com/ossrs/ai-talk/releases/tag/v1.0.23)
* Support setup chat AI model for each robot. v1.0.24
* Support setup chat window for each robot. [v1.0.25](https://github.com/ossrs/ai-talk/releases/tag/v1.0.25)
* Alert error when not HTTPS. v1.0.26
* Support HTTPS proxy for OpenAI. v1.0.27
* Support official OpenAI API without proxy. [v1.0.28](https://github.com/ossrs/ai-talk/releases/tag/v1.0.28)
* Always use HTTPS proxy if not specified. v1.0.29
* Do not require some variables. v1.0.30
* Refine the logs for user and bot. v1.0.31
* Detect user silent and warning. v1.0.32
* Refine bot log for the first time. [v1.0.33](https://github.com/ossrs/ai-talk/releases/tag/v1.0.33)
* Allow user retry when error. v1.0.34
* Refine badcase for user input. v1.0.35
* Fix bug for setting window for robot. [v1.0.36](https://github.com/ossrs/ai-talk/releases/tag/v1.0.36)
* Support setup API proxy and key for ASR,Chat,TTS. v1.0.37
* Support Tencent Speech to speed up. v1.0.37
* Support share logging text mode. v1.0.38
* Fix some badcase for sentence determine. [v1.0.39](https://github.com/ossrs/ai-talk/releases/tag/v1.0.39)
* Speed up the ASR, without transcode. v1.0.40
* Refine the stat for elapsed time cost of upload. v1.0.41
* Refine the log label. [v1.0.46](https://github.com/ossrs/ai-talk/releases/tag/v1.0.46)
* Support OpenAI organization. v1.0.47

Winlin, 2023.12
