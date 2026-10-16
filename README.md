# Go city guide

This repository has a Go HTTP server. The server sends travel recommendations for one city. The server calls Vercel AI Gateway. The weather tool gets the forecast.

This file is for persons at Vercel. The text for customers is in `build-an-ai-powered-go-api-with-ai-gateway-on-vercel.md`. That file is a draft. The draft has `[VERIFY]` marks. Do those checks before you send the draft to customers.

This repository is an example. Do not use this server as a production service.

## Description

The example has four parts:

- The framework preset for Go builds the `net/http` server as one Vercel Function.
- The OpenAI Go client calls AI Gateway at `https://ai-gateway.vercel.sh/v1`.
- The model must call the tool `get_forecast` before the final answer.
- The response schema has `Strict` set to true. The model must return JSON for the `Recommendation` structure.

The server reads the model one time, when the process starts. The server selects the AI Gateway credential for each request.

## Files

| File | Purpose |
| --- | --- |
| `main.go` | HTTP routes, request checks, and credential selection |
| `agent.go` | AI Gateway client, tool loop, and response schema |
| `weather.go` | Open-Meteo forecast tool |
| `agent_test.go` | Tests for the tool loop and for credentials |
| `vercel.json` | Sets `framework` to `go` |
| `go.mod` | Module path and Go version |
| `go.sum` | Module checksums |
| `build-an-ai-powered-go-api-with-ai-gateway-on-vercel.md` | Draft guide for customers |

The module path in `go.mod` is `github.com/your_github_username_here/go-city-guide`. That path is an example path. It is not the path of this repository. You can run `go build` with this path.

## Requirements

You must have:

- Go 1.26 or later. The `go.mod` file sets the Go version to 1.26.0.
- An API key for AI Gateway. You need the key to start the server on your computer. Get the key from the AI Gateway tab in the team dashboard.
- The Vercel CLI, if you deploy from your computer.

## Start the server on your computer

1. Set the API key. Replace `your_key` with your key.

   ```bash
   export AI_GATEWAY_API_KEY=your_key
   ```

2. Start the server.

   ```bash
   go run .
   ```

The server uses port 3000. Set `PORT` before you start the server if you need a different port.

3. Read the server status.

   ```bash
   curl -s localhost:3000/healthz
   ```

The response is JSON. The `model` field is the model for this process.

4. Send a recommendation request.

   ```bash
   curl -s -X POST localhost:3000/recommendations \
     -H 'Content-Type: application/json' \
     -d '{"city":"Lisbon","days":2,"interests":["food"]}'
   ```

## Environment variables

| Name | Purpose |
| --- | --- |
| `AI_GATEWAY_API_KEY` | API key for calls from your computer. If this value is not empty, the server uses this key first. |
| `VERCEL_OIDC_TOKEN` | OIDC token after `vercel env pull`. The server uses this token last. |
| `AI_MODEL` | Model ID. If you do not set this variable, the model is `anthropic/claude-haiku-5.5`. |
| `PORT` | Port for the HTTP server. If you do not set this variable, the port is `3000`. Vercel sets this value. |

To see model IDs, send a GET request to `https://ai-gateway.vercel.sh/v1/models`.

Set `AI_MODEL` before the process starts. The process keeps that value until the process stops. On Vercel, a new process reads the value when the process starts.

## Credential selection

`gatewayCredential` in `main.go` uses this order:

1. `AI_GATEWAY_API_KEY`, if the value is not empty.
2. The request header `x-vercel-oidc-token`, if the header is not empty.
3. `VERCEL_OIDC_TOKEN`.

Read the credential for each request. Do not read the token one time in `main`. On Vercel, each request to the Vercel Function has an OIDC token in the header `x-vercel-oidc-token`.

The draft guide says that Vercel can reuse one token for not more than 90 minutes. The same guide says that the token life is two hours.

CAUTION: Do not set `AI_GATEWAY_API_KEY` on the Vercel project if the Vercel Function must use OIDC. If AI Gateway does not accept the key, the response status is HTTP 502. The log shows HTTP 401 from AI Gateway.

If the server has no credential, the handler sends HTTP 500. The error text is `server is not configured for AI Gateway`.

## HTTP interface

The server has two routes: `GET /healthz` and `POST /recommendations`.

`http.ServeMux` sends HTTP 404 for a path with no route. `http.ServeMux` sends HTTP 405 for an incorrect method on a known path.

### GET /healthz

An example of the response:

```json
{"status":"ok","model":"anthropic/claude-haiku-5.5"}
```

The `model` value is the model for this process.

### POST /recommendations

The request body has these fields:

| Field | Rule |
| --- | --- |
| `city` | You must send this field. The value is a string. An empty string is not correct. |
| `days` | You can send the request without this field. If the value is `0` or less, the server uses `3`. If the value is more than `14`, the server sends HTTP 400. |
| `interests` | You can send the request without this field. The value is a list of strings. |

The HTTP 200 response has this structure:

```json
{
  "city": "Lisbon",
  "summary": "string",
  "recommendations": [
    {
      "name": "string",
      "category": "food",
      "description": "string",
      "bestTime": "string",
      "weatherDependent": false
    }
  ]
}
```

`category` must be one of these values:

- `food`
- `culture`
- `outdoors`
- `nightlife`
- `shopping`
- `family`

The prompt tells the model to return 3 to 8 recommendations. The schema does not limit that count.

| Status | Condition | Error text |
| --- | --- | --- |
| 400 | The body is not JSON. | `invalid JSON body` |
| 400 | `city` is empty. | `city is required` |
| 400 | `days` is more than 14. | `days must be 14 or fewer` |
| 500 | The server has no AI Gateway credential. | `server is not configured for AI Gateway` |
| 502 | AI Gateway or the agent returns an error. | `recommendation agent failed` |

The server writes the Gateway error to the log. The server does not send that error text to the caller.

## Agent sequence

The code for this sequence is in `agent.go`.

1. The handler selects the credential.
2. The agent sends the system message, the user message, the tool schema, and the response schema to AI Gateway.
3. The model can ask for the tool `get_forecast`.
4. The server gets a forecast for 7 days from Open-Meteo.
5. The server sends the tool result to the model.
6. The model returns JSON and no tool call. The server then converts that JSON into `Recommendation`.

If the model does not finish in 5 steps, the agent returns an error. The handler then sends HTTP 502.

Open-Meteo does not need an API key. The tool calls these endpoints:

- `https://geocoding-api.open-meteo.com/v1/search`
- `https://api.open-meteo.com/v1/forecast`

The tool uses the first geocoding result. A city name can identify more than one place. The first place can be incorrect.

If the tool fails, `runTool` returns a JSON error. The agent does not stop the loop for that error. The model can read the error in the next step.

The prompt shows `interests` in the Go default format. The text looks like `[food]`.

## Tests

The tests do not call AI Gateway. `TestRecommendToolLoop` uses an `httptest` server.

1. Run the tests.

   ```bash
   go test ./...
   ```

## Deploy to Vercel

1. Put `go.sum` in the Git repository.

   The Go preset does not download a module that has no checksum.

2. Keep `"framework": "go"` in `vercel.json`.

   The preset must have this property.

3. Run `vercel` to make a preview deployment.
4. Run `vercel --prod` to make a production deployment.

This repository does not confirm the exact prompts from `vercel`. See the `[VERIFY]` mark in the draft guide.

If you connect the Git repository in the Vercel project settings, each Git push starts a deployment.

You do not add an AI Gateway key when the Vercel Function uses OIDC. Each request to the Vercel Function has an OIDC token in the header `x-vercel-oidc-token`.

`vercel.json` does not set `maxDuration`. You do not need `maxDuration` for this weather tool. Add `maxDuration` if you add a slow tool.

## Deployment Protection

New Vercel projects use Standard Protection. If Deployment Protection does not accept the request, the response status is HTTP 401. The message is `Protected by Vercel Authentication`.

Do not change Standard Protection. This server has no authentication in the Go code.

For a test from your terminal, use the Protection Bypass for Automation secret. Send the secret in the header `x-vercel-protection-bypass`.

For a caller that is a service, add the caller under Trusted Sources. The caller sends its OIDC token in the header `x-vercel-trusted-oidc-idp-token`. If Vercel does not accept the token, the response status is HTTP 401.

Standard Protection does not protect a production custom domain. If you add a custom domain, add authentication in the Go code.

This repository does not confirm the Trusted Sources menu labels. See the `[VERIFY]` mark in the draft guide.

## Related documents

Refer to these documents:

- [Go runtime](https://vercel.com/docs/functions/runtimes/go)
- [AI Gateway OpenAI-compatible API](https://vercel.com/docs/ai-gateway/openai-compat)
- [AI Gateway authentication](https://vercel.com/docs/ai-gateway/authentication-and-byok)
- [Vercel OIDC](https://vercel.com/docs/oidc)
- [Deployment Protection](https://vercel.com/docs/deployment-protection)
- [Trusted Sources](https://vercel.com/docs/deployment-protection/methods-to-bypass-deployment-protection/trusted-sources)
