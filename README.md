# AI Talk

AI-Talk allows you to talk with OpenAI GPT.

<details>
<summary>General Assistant</summary>

Prompt:
```
AI_SYSTEM_PROMPT=You are an assistant
```

https://github.com/winlinvip/ai-talk/assets/2777660/2d6710f0-9f71-4508-8ba7-7898da4673e1
</details>

<details>
<summary>Spoken English Coach</summary>

Prompt:
```
AI_SYSTEM_PROMPT=I want you to act as a spoken English teacher and improver. I will speak to you in English and you will reply to me in English to practice my spoken English. I want you to keep your reply neat, limiting the reply to 100 words. I want you to strictly correct my grammar mistakes, typos, and factual errors. I want you to ask me a question in your reply. Now let's start practicing, you could ask me a question first. Remember, I want you to strictly correct my grammar mistakes, typos, and factual errors.
```
    
https://github.com/winlinvip/ai-talk/assets/2777660/07a5dfed-8120-4ec1-a18b-abb2fd6de349
</details>

<details>
<summary>Simultaneous English Translation</summary>

Prompt:
```
AI_SYSTEM_PROMPT=Translate to simple and easy to understand english. Never answer questions but only translate text to English.
```

https://github.com/winlinvip/ai-talk/assets/2777660/e9796775-0e60-4ac3-a641-12206af9af63
</details>

<details>
<summary>Chinese Words Solitaire Game</summary>

Prompt:
```
AI_ASR_LANGUAGE=zh
AI_SYSTEM_PROMPT=我希望你是一个儿童的词语接龙的助手。我希望你做两个词的词语接龙。我希望你不要用重复的词语。我希望你回答比较简短，不超过50字。我希望你重复我说的词，然后再接龙。我希望你回答时，解释下词语的含义。请记住，你讲的答案是给6岁小孩听得懂的。请记住，你要做词语接龙。例如：我：苹果。你：苹果，果园。苹果，是一种水果，长在树上，是红色的。果园，是一种地方，有很多树，有很多果子。
```

https://github.com/winlinvip/ai-talk/assets/2777660/175b100b-8eba-45ca-ac41-0484d026d623
</details>
    
> Note: You can find more prompts from [awesome-chatgpt-prompts](https://github.com/f/awesome-chatgpt-prompts).

## Usage

To run in docker:

```bash
docker run --rm -it -p 80:3000 -p 443:3443 \
    -e OPENAI_API_KEY=sk-xxx -e OPENAI_PROXY=api.openai.com \
    -e AI_SYSTEM_PROMPT="You are an assistant" -e AI_MODEL="gpt-4-1106-preview" \
    ossrs/ai-talk
```

> Note: Setup the `OPENAI_PROXY` if you are not able to access the API directly.

> Note: Please use `registry.cn-hangzhou.aliyuncs.com/ossrs/ai-talk` in China.

Then you can access by http://localhost and happy to talk with AI.

Then you can access by https://your-server-ip from your mobile browser.

> Note: The HTTPS certificate is self-signed, you need to accept it in your mobile browser.

## Environment Variables

Required environment variables:

* `OPENAI_API_KEY`: The OpenAI API key, get from [https://platform.openai.com/api-keys](https://platform.openai.com/api-keys).

Optional environment variables:

* `OPENAI_PROXY`: The OpenAI API proxy, default to `api.openai.com`, which directly access OpenAI API without proxy.
* `AI_SYSTEM_PROMPT`: The system prompt, default to `You are a helpful assistant.`.
* `AI_MODEL`: The AI model, default to `gpt-4-1106-preview` which is the latest model.
* `AI_ASR_LANGUAGE`: The language for Whisper ASR, default to `en`, see [ISO-639-1](https://en.wikipedia.org/wiki/List_of_ISO_639-1_codes), bellow are some examples:
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

Other optional environment variables:

* `HTTP_LISTEN`: The HTTP listen address, default to `:3000`, please use `-p 80:3000` to map to a different port.
* `HTTPS_LISTEN`: The HTTPS listen address, default to `:3443`, please use `-p 443:3443` to map to a different port.
* `PROXY_STATIC`: Whether proxy to static files, default to `false`.
* `AI_NO_PADDING`: Whether disable padding text, default to `false`.
* `AI_PADDING_TEXT`: The padding text, default to `My answer is `.
* `AI_MAX_TOKENS`: The max tokens, default to `1024`.
* `AI_TEMPERATURE`: The temperature, default to `0.9`.
* `KEEP_AUDIO_FILES`: Whether keep audio files, default to `false`.

Winlin, 2023.12
