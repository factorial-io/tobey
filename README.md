# Tobey, a robust and scalable Crawler

The service is entirely stateless and receives requests to crawl a website via a
simple HTTP API. Once a resources has been downloaded, forwards the results to a
webhook, if one is configured.

In its simplest form the service just receives a root URL of the website to be
crawled.

The service vertical scaling can be controlled by the number of workers used for
crawling. The service is horizontally scalable by adding more instances on nodes
in a cluster. In horizontal scaling, any instances can receive crawl requests,
for easy load balancing. The instances will coordinate with each other via a
RabbitMQ.

## Features

- No configuration required.
- Simple HTTP API to submit crawl requests.
- Scalable, horizontally and vertically.
- Stateless, no data store required, as nothing is persisted.
- No further service dependencies, when operating as a single instance.
- Detects and uses a sitemap and robots.txt automatically (can be disabled).
- Per host rate limiting, even when multiple instances are used.
- Full support for OpenTelemetry.

## Quickstart

To quickly try out the service, ensure you have Go installed. And run the following commands:

```sh
# In the first terminal start the service.
go run . 

# In another terminal, submit a crawl request.
curl -X POST http://127.0.0.1:8080 \
     -H 'Content-Type: application/json' \
     -d '{"url": "https://www.example.org/"}'
```

## Configuration

The service is configured via environment variables. The following environment
variables are available:

| Variable Name  | Default Value  | Supported Values | Description                      |
|----------------|----------------|------------------|----------------------------------|
| `TOBEY_DEBUG` | `false` | `true`, `false`  | Controls debug mode. |
| `TOBEY_SKIP_CACHE` | `false` | `true`, `false`  | Controls caching access. |
| `TOBEY_RABBITMQ_DSN` | empty | i.e. `amqp://guest:guest@rabbitmq:5672/` | DSN to reach a RabbitMQ instance. Only needed when operating multiple instances. |
| `TOBEY_REDIS_DSN` | empty | i.e. `redis://localhost:6379` | DSN to reach a Redis instance. Only needed when operating multiple instances. |
| `TOBEY_PROGRESS_DSN` | empty | i.e. `http://localhost:9020`  | DSN where to reach a progress service. When configured tobey will send progress updates there. |
| `TOBEY_REQS_PER_S` | 2 | i.e. `4`  | Maximum number of allowed requests per second per host.  |
| `TOBEY_TELEMETRY` | empty | i.e. `metrics traces` | Space separated list of what kind of telemetry is emitted. |

On top of these variables, the service's telemetry
feature can be configured via the commonly known 
[OpenTelemetry environment variables](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/).

## Submitting a Basic Crawl Request

Tobey currently has a single API endpoint to receive crawl requests: `/`.

In its most simple form you submit a single URL that is used as the entry
point to for the crawling process. This will discover further resource URLs to
crawl automatically by looking at the sitemap - if one is available - and by
extracting links for content of the webpages.

```jsonc
{
  "url": "https://example.org"
}
```

### Constraining Crawling

#### Domains

By default and when crawling a whole website tobey will only download resources
from the host as provided in the URL, this is so we don't end up downloading the
whole internet. You may additionally provide host domains that are an alias to
the URLs domain that we will download from.

Please note that it's enough to provide the "naked" version of the domain
without the "www" subdomain. Providing "example.org" implies "www.example.org"
as well as all other subdomains. Providing a domain with "www." prefix will also
allow the naked domain (and all its subdomains).

```jsonc
{
  "url": "https://example.org",
  "domains": [ // Works as an allow list.
    "example.org", // Only crawl on these domains...
    "example.com", // Entirely different domain, but same content.
  ]
}
```

### Paths

To skip resources with certain paths, you may provide a list of path segments to
include or skip via the `paths` or `!paths` key. The path segments may appear
anywhere in the full URL path.

```jsonc
{
  "url": "https://example.org",
  "paths": [
    "/en/", // Only crawl resources under /en/.
  ],
  "!paths": [
    "/search/", // Do not crawl search resources.
  ]
}
```

As you can see positive and negative path constraints can be combined. With the options
given above, `/en/about-us/` would be crawled, but not `/en/search/` and not `/blog/article`.

### Run Identifiers

Each time you submit a URL to be crawled, a "run" is internally created. Tobey
automatically creates a unique run UUID for you, when you don't submit one
yourself. You'll receive that created run UUID in the response when submitting a
URL to be crawled.

When you already have a run UUID yourself, you may as well submit in the crawl
request, than your run UUID will be used internally and visible when results are
dispatched.

```jsonc
{
  "url": "https://example.org",
  "run_uuid": "0033085c-685b-432a-9aa4-0aca59cc3e12"
}
```

### Multiple URLs

Multiple URLs either as entrypoints or for oneshot downloading work a well,
using the `urls` key:

```jsonc
{
  "urls": [
    "https://example.org/blog", 
    "https://example.org/values"
  ]
}
```

### Bypassing robots.txt

When running certain tests you might want to bypass the robots.txt file. You can
do so by providing the `skip_robots` key:

```jsonc
{
  "url": "https://example.org",
  "skip_robots": true
}
```

### Sitemaps

Tobey will automatically discover and use a sitemap if one is available, either
by looking at well known locations or by looking at the robots.txt.

If you don't want the crawler to use sitemaps at all, you may disable this
behavior by providing the `skip_sitemap_discovery` key:

```jsonc
{
  "url": "https://example.org",
  "skip_sitemap_discovery": true
}
```

If you prefer to provide the sitemap URL yourself, you may do so by providing
the URL under the `url` key algonside the entrypoint:

```jsonc
{
  "urls": [
    "https://example.org", 
    "https://example.org/sitemap.xml"
  ],
  "skip_auto_sitemaps": true
}
```

### Authentication

When the resource you want to download requires authentication, you can provide
the credentials for HTTP basic auth in the URL. The crawler will use these
credientials for all resources under the same domain.

```jsonc
{
  "url": "https://foo:secret@example.org"
}
```

When you want to provide the credentials in a more structured way, you can do so
by providing the `auth` key:

```jsonc
{
  "url": "https://example.org"
  "auth": [
    { host: "example.org", method: "basic", username: "foo", password: "secret" }
  ]
```

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
  "url": "https://example.org/values"
}
```

### Using Webhook to state where results should go

[Webhooks](https://mailchimp.com/en/marketing-glossary/webhook) are a technique to notify other services about a result, once its ready.

Once the crawlwer has results for a resource, it will deliver them to a webhook,
if one is configured via the `webhook` key. Using the `data` key you can pass
through additional information to the target of the webhook.

```jsonc
{
  // ...
  "url": "https://example.org",
  // ...
  "webhook": {
    "endpoint": "https://metatags.example.org/accept-webhook",
    "data": { // Any additional data that you want the hook to receive.
      "magic_number": 12 
    }
  }
}
```

This is how the payload will look like, and how it is received by the target:

```jsonc
{
  "action": "collector.response",
  "run_uuid": "0033085c-685b-432a-9aa4-0aca59cc3e12",
  // ... 
  "request_url": "http://...", 
  "response_body": "...", // Base64 encoded raw response body received when downloading the resource.
  // ... 
  "data": { // Passed-through data.
    "magic_number": 12 
  },
}
```
