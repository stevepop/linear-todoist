# Linear-Todoist

A lightweight webhook server that automatically creates Todoist tasks when Linear issues are assigned to you.

## How it works

1. Linear sends a webhook to your server when an issue is created or updated.
2. The server verifies the webhook signature, checks if the issue is assigned to you, and creates a corresponding task in your Todoist project.
3. Duplicate tasks are skipped by checking existing tasks in the target project.

## Prerequisites

- Go 1.25+
- A [Linear](https://linear.app) account with API access
- A [Todoist](https://todoist.com) account with an API token

## Setup

### 1. Clone the repository

```sh
git clone https://github.com/yourusername/linear-todoist.git
cd linear-todoist
```

### 2. Configure environment variables

Copy the example and fill in your values:

```sh
cp .env.example .env
```

| Variable | Description |
|---|---|
| `LINEAR_WEBHOOK_SECRET` | Webhook signing secret from Linear (Settings > API > Webhooks) |
| `LINEAR_USER_ID` | Your Linear user ID (found in your Linear profile URL or via the API) |
| `TODOIST_API_TOKEN` | Your Todoist API token (Settings > Integrations > Developer) |
| `TODOIST_PROJECT_ID` | The Todoist project ID where tasks will be created |
| `PORT` | Server port (defaults to `8080`) |

### 3. Create a Linear webhook

1. Go to **Linear Settings > API > Webhooks**.
2. Create a new webhook pointing to `https://<your-domain>/webhooks/linear`.
3. Select **Issues** as the resource type.
4. Copy the signing secret into your `.env` file.

### 4. Run

```sh
go run main.go
```

Or build and run the binary:

```sh
go build -o linear-todoist
./linear-todoist
```

### 5. Expose your server

For local development, use a tunnel like [ngrok](https://ngrok.com) or [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/):

```sh
ngrok http 8080
```

Use the generated URL as your Linear webhook endpoint.

## Task format

Created Todoist tasks follow this format:

- **Title:** `[ISSUE-ID] Issue title`
- **Description:** Link to the Linear issue
- **Due:** Today at 6pm (Default due time)

## License

MIT
