# Tobey, Website Standards URL Spider & Fetcher Service

A service that exposes a simple HTTP API. Given a root URL to a site will spider
the site for more URLs. Given an URL to a resource on the web, can be i.e. a web
page or a XML sitemap, will fetch and return it's contents.

## Features

- Combines spidering and crawling as there is overlap functionality wise, and it
  makes sense to use the same code infra for that.
- Support both JavaScript-rendererd fetch and plain fetch, on a per URL basis as
  some clients might be on a free plan and in-browser-rendering consumes a lot of
  resources.
- Doesn't need any client libraries to connect to it, it's API is straight-forward and simple. 
- Batteries included, doesn't depend on external resources or scheduling.
- Does scheduling on its own.
- Honors robots.txt
- Detects and uses a sitemap automatically, to enhance spidering quality.

## Quickstart

To quickly try out the service, ensure you have Go installed. And run the following commands:

```sh
# In the first terminal start the service.
go run .

# In another terminal, submit a crawl request.
curl -X POST http://127.0.0.1:8080/sites \
     -H 'Content-Type: application/json' \
     -d '{"url": "https://www.factorial.io/"}'
```

## Spider Request

### Crawling a whole website

In its most simplest form, submitting a URL to the root of a website is enough.
Information about the site itself are automatically derived. This will perform
a crawl of the entire webpage and discover URLs automatically.

```json
{
  "url": "https://factorial.io"
}
```

The explicit form of the above is as follows. The `site` key allows you to be more explicit.

```json
{
  "site": {
    "domain": "factorial.io",
    "root": "https://factorial.io"
  }
}
```

### Using Webhook to state where results should go

[Webhooks](https://mailchimp.com/en/marketing-glossary/webhook) are a technique to notify other services about a result, once its ready.

Once the spider has results for a webpage, it will deliver them to a webhook,
if one is configured via the `webhook` key. Using the `data` key you can pass
through additional information to the target of the webhook.

```jsonc
{
  // ...
  "url": "https://factorial.io",
  // ...
  "webhook": {
    "url": "https://metatags.factorial.io/accept-webhook",
    "data": { // Any additional data that you want the hook to receive.
      "test_run": 12 
    }
  }
}
```

This is how the payload will look like, and how it is received by the target:

```jsonc
{
  "action": "page-result",
  "data": { // Passed-through data.
    "test_run": 12
  } 
  // ...
  "url": "https://factorial.io/en/blog/prototyping-is-fun" // The URL of the web page.
  // ...
  "site": { // The site to which the webpage belongs.
    "domain": "factorial.io",
    // ...
  },
  // ...
}
```
 

