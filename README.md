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
