# AI Talk

AI-Talk allows you to talk with OpenAI GPT.

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

Other optional environment variables:

* `HTTP_LISTEN`: The HTTP listen address, default to `:3000`, please use `-p 80:3000` to map to a different port.
* `HTTPS_LISTEN`: The HTTPS listen address, default to `:3443`, please use `-p 443:3443` to map to a different port.
* `PROXY_STATIC`: Whether proxy to static files, default to `false`.
* `AI_NO_PADDING`: Whether disable padding text, default to `false`.
* `AI_PADDING_TEXT`: The padding text, default to `My answer is `.
* `AI_MAX_TOKENS`: The max tokens, default to `1024`.
* `AI_TEMPERATURE`: The temperature, default to `0.9`.