# AI Talk

AI-Talk allows you to talk with OpenAI GPT.

## Usage

Create a environment file `.env` and setup your OpenAI API key:

```bash
OPENAI_API_KEY=sk-xxx
OPENAI_PROXY=api.openai.com
```

> Note: Setup the `OPENAI_PROXY` if you are not able to access the API directly.

Then run the backend:

```bash
(cd backend && go run . )
```

And the frontend:

```bash
npm install && npm start
```

Then you can access by http://localhost:3000 and happy to talk with AI.

## For Mobile Browser

If you want to access from mobile browser, you need to setup an HTTPS server:

```bash
go install github.com/ossrs/go-oryx/httpx-static@latest &&
openssl genrsa -out server.key 2048 &&
subj="/C=CN/ST=Beijing/L=Beijing/O=Me/OU=Me/CN=me.org" &&
openssl req -new -x509 -key server.key -out server.crt -days 365 -subj $subj &&
$HOME/go/bin/httpx-static -k server.key -c server.crt -t 80 -s 443 -r . -p http://localhost:3000
```

Then you can access by https://your-server-ip from your mobile browser.

> Note: The HTTPS certificate is self-signed, you need to accept it in your mobile browser.
