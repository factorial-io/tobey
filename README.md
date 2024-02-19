# Tobey, Website Standards URL Spider & Fetcher Service

A service that exposes a simple HTTP API. Given a root URL to a site will spider
the site for more URLs. Given an URL to a resource on the web, can be i.e. a web
page or a XML sitemap, will fetch and return it's contents.

## Features

- Combines spidering and crawling as there is overlap functionality wise, and it
  makes sense to use the same code infra for that.
- Doesn't need any client libraries to connect to it, it's API is straight-forward and simple. 
- Batteries included, doesn't depend on external resources or scheduling.
- Honors robots.txt
- Detects and uses a sitemap automatically, to enhance spidering quality.

## Implementation Details

The spider is implemented on to of the colly, a crawling framework for Go. We've
chose this framework, as it allows us to outgrow it gradually. In the current
version of tobey, we use our own implementation for queuing, that doesn't
require modifications to the original framework code. However as we have to provide
our own cache storage implementation we forked the framework to `internal`.

A future version will support JavaScript-rendered fetch alongside plain fetch.

Scheduling of re-crawls isn't (yet) part of the implementation, as we're still
finding the right place (inside/outside) for this functionality.

## Quickstart

To quickly try out the service, ensure you have Go installed. And run the following commands:

```sh
# In the first terminal start the service.
make dev

# In another terminal, submit a crawl request.
curl -X POST http://127.0.0.1:8080 \
     -H 'Content-Type: application/json' \
     -d '{"url": "https://www.factorial.io/"}'
```

## Submitting a Basic Crawl Request

Tobey currently has a single API endpoint to receive crawl requests: `/`.

In its most simple form you submit a single URL that is used as the entry
point to for the spidering process. This will discover further URLs to crawl
automatically by looking at the sitemap - if one is available - and by
extracting links for content of the webpages.

```jsonc
{
  "url": "https://factorial.io"
}
```

### Constraining Spidering

When spidering a whole website tobey will only download resources from the
host as provided in the URL, this is so we don't end up downloading the whole
internet. You may additionally provide host domains that are an alias to the
URLs domain that we will download from.

Please note that it's enough to provide the "naked" version of the domain
without the "www" subdomain. Providing "example.org" implies "www.example.org"
as well as all other subdomains. Providing a domain with "www." prefix will also
allow the naked domain (and all its subdomains).

```jsonc
{
  "url": "https://example.org",
  "domains": [
    "example.org",
    "example.com", // Entirely different domain, but same content.
  ]
}
```

### Run Identifiers

Each time you submit a URL to be crawled, a "run" is internally created. Tobey
automatically creates a unique run ID for you, when you don't submit one
yourself. You'll receive that created run ID in the response when submitting a
URL to be crawled.

When you already have a run ID yourself, you may as well submit in the crawl
request, than your run ID will be used internally and visible when results are
dispatched.

```jsonc
{
  "url": "https://example.org",
  "run_id": 23, // or "0033085c-685b-432a-9aa4-0aca59cc3e12"
}
```

The run ID can be either a UUID as a string or number or any other unique number.


### Prioritites (not implemented)

tbd

### Sample Size (not implemented)

When a sample size is given, and its threshold of crawled pages has been reached
the crawl request will stop fetching more pages. Please note that slightly more pages
than the sample size might be returned.

```jsonc
{
  "url": "https://example.org",
  "sample_size": 10
}
```

### Oneshot Mode (not implemented)

By default the URLs submitted are considered entrypoints, you can change this
behavior by providing the query parameter `oneshot`. This will only download the
resource as found under the URL and nothing more. Of course multiple URLs (see
below) are usable here as well.

```sh
curl -X POST http://127.0.0.1:8080?oneshot # ...
```

```jsonc
{
  "url": "https://factorial.io/values"
}
```

### Multiple URLs (not implemented)

Multiple URLs either as entrypoints or for oneshot downloading work a well,
using the `urls` key:

```jsonc
{
  "urls": [
    "https://factorial.io/blog", 
    "https://factorial.io/values"
  ]
}
```

### Using Webhook to state where results should go

[Webhooks](https://mailchimp.com/en/marketing-glossary/webhook) are a technique to notify other services about a result, once its ready.

Once the spider has results for a resource, it will deliver them to a webhook,
if one is configured via the `webhook` key. Using the `data` key you can pass
through additional information to the target of the webhook.

```jsonc
{
  // ...
  "url": "https://factorial.io",
  // ...
  "webhook": {
    "endpoint": "https://metatags.factorial.io/accept-webhook",
    "data": { // Any additional data that you want the hook to receive.
      "test_run": 12 
    }
  }
}
```

This is how the payload will look like, and how it is received by the target:

```jsonc
{
  "action": "tobey.result",
  "data": { // Passed-through data.
    "test_run": 12
  },
  "crawl": 123,            // ID of the crawl that triggered the download of this resource.
  "request": {/* ... */},  // Raw request submitted to download the resource.
  "response": {/* ... */}, // Raw response received when downloading the resource.
  // ... 
}
```

| Environment Variabel        |  Default Value           |
| ------------- |:-------------:|
| RABBIT_USER     | rabbitmq |
| RABBIT_PW      | rabbitmq     |
| RABBIT_HOST | localhost     |
| RABBIT_PORT | 5672     |
|SERVICE_DISCOVERY | localhost|
|OWN_ADDRESS| ""|
|GRPC_PORT|6543|
|OWN_PORT| 8000|
|SERVICE_DISCOVERY_NAMESPACE| workers|
|SERVICE_DISCOVERY_TAGS| drupal_workers|
|PHP_FILE_PATH| ./src/Command/Worker.php"|
