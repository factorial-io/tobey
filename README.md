# Tobey, a robust and scalable Crawler

Tobey is a throughput optimizing web crawler, that is scalable from a single instance to a cluster. It features intelligent 
rate limiting, distributed coordination, and flexible deployment options.

## Running Tobey

Start the service.
```sh
go run . 
```

In its simplest form the service just receives a root URL of the website to be
crawled. 

```sh
curl -X POST http://127.0.0.1:8080 \
     -H 'Content-Type: application/json' \
     -d '{"url": "https://www.example.org/"}'
```

## Deployment Options

### Dependency Free

By default Tobey runs without any dependencies on any other service. In this mode
the service will not coordinate with other instances. It will store results locally 
on disk, but not report any progress. If you are trying out tobey this is the
easiest way to get started.

```sh
TOBEY_RESULTS_DSN=disk:///path/to/results go run .
```

### Stateless Operation

It is possible to configure and use Tobey in a stateless manner. In this operation mode
you'll specify configuration on a per-run basis, and not statically via a configuration file. Choosing 
the webhook results store will forward results to a webhook endpoint without storing them locally.

```sh
TOBEY_RESULTS_DSN=webhook://example.org/webhook?enable_dynamic_config=true go run .
```

### Distributed Operation

The service is horizontally scalable by adding more instances on nodes
in a cluster. In horizontal scaling, any instances can receive crawl requests,
for easy load balancing. The instances will coordinate with each other via Redis.

```sh
TOBEY_REDIS_DSN=redis://localhost:6379 go run .
```

## Scaling

Tobey can be scaled vertically by increasing the number of workers, via the `WORKERS` environment variable, or horizontally 
by adding more instances in a cluster, see the [Distributed Operation](#distributed-operation) section for more details.

The crawler is designed to handle a large potentially infinite number of hosts,
which presents challenges for managing resources like memory and concurrency.
Keeping a persistent worker process or goroutine for each host would be
inefficient and resource-intensive, particularly since external interactions
can make it difficult to keep them alive. Instead, Tobey uses a pool of workers
that can process multiple requests per host concurrently, balancing the workload
across different hosts.

## Smart Rate Limiting

The Tobey Crawler architecture optimizes throughput per host by dynamically
managing rate limits, ensuring that requests to each host are processed as
efficiently as possible. The crawler does not impose static rate limits;
instead, it adapts to each host's capabilities, adjusting the rate limit in real
time based on feedback from headers or other factors. 

This dynamic adjustment is essential. To manage these rate limits
effectively, Tobey employs a rate-limited work queue that abstracts away the
complexities of dynamic rate limiting from other parts of the system. The goal
is to focus on maintaining a steady flow of requests without overwhelming
individual hosts.

## Caching

Caching is a critical part of the architecture. The crawler uses a global cache,
for HTTP responses. Access to sitemaps and robot control files are also cached.
While these files have expiration times, the crawler maintains an in-memory
cache to quickly validate requests without constantly retrieving them. The cache
is designed to be updated or invalidated as necessary, and a signal can be sent
across all Tobey instances to ensure the latest robot control files are used,
keeping the system responsive and compliant. This layered caching strategy,
along with the dynamic rate limit adjustment, ensures that Tobey maintains high
efficiency and adaptability during its crawling operations.

## Limitations

Also Tobey can be configured - on a per run basis - to crawl websites behind
HTTP basic auth, **it does not support fetching personalized content**. It is
expected that the website is generally publicly available, and that the content
is the same for all users. When HTTP basic auth is used by the website it must
only be so in order to prevent early access.

## Configuration

The service is configured via environment variables. The following environment
variables are available:

| Variable Name  | Default Value  | Supported Values | Description                      |
|----------------|----------------|------------------|----------------------------------|
| `TOBEY_DEBUG` | `false` | `true`, `false`  | Controls debug mode. |
| `TOBEY_SKIP_CACHE` | `false` | `true`, `false`  | Controls caching access. |
| `TOBEY_REDIS_DSN` | empty | i.e. `redis://localhost:6379` | DSN to reach a Redis instance. Only needed when operating multiple instances. |
| `TOBEY_PROGRESS_DSN` | empty | `factorial://host:port`, `noop://` | DSN for progress reporting service. When configured, Tobey will send progress updates there. The factorial scheme enables progress updates to a Factorial progress service. Use noop:// to explicitly disable progress updates. |
| `TOBEY_RESULTS_DSN` | empty | `disk:///path`, `webhook://host/path`, `noop://` | DSN specifying where crawl results should be stored. Use disk:// for local filesystem storage, webhook:// to forward results to an HTTP endpoint, or noop:// to discard results. |
| `TOBEY_TELEMETRY` | empty | i.e. `metrics traces` | Space separated list of what kind of telemetry is emitted. |

On top of these variables, the service's telemetry
feature can be configured via the commonly known 
[OpenTelemetry environment variables](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/).

## Providing Crawl Targets

### Submitting a Basic Crawl Request

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


### Authentication

When the target you want to crawl requires authentication, you can provide
the credentials for HTTP basic auth in the URL. The crawler will use these
credientials for all resources under the same domain for that run.

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
}
```

### Domain Constraints

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

### Path Constraints

To skip resources with certain paths, you may provide a list of literal path
segments to include or skip via the `paths` or `!paths` key. The path segments
may appear anywhere in the full URL path. Alternatively to literal fragments
you may also use regular expressions.

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

## Triggering Runs

Each time you submit a URL to be crawled, a _Run_ is internally created. Tobey
automatically creates a unique run UUID as **a run identifier** for you. You may
specify your own run UUID as well. 

```jsonc
{
  "url": "https://example.org",
  "run_uuid": "0033085c-685b-432a-9aa4-0aca59cc3e12" // optional
  // ...
}
```

You may also attach metadata to a run. This metadata will be attached to all results
from that run. 

```jsonc
{
  "url": "https://example.org",
  // ...
  "metadata": {
    "internal_project_reference": 42,
    "triggered_by": "user@example.org"
  }
}
```

## Collecting Results

Tobey currently supports multiple methods to handle results. You can either store
them locally on disk, or forward them to a webhook endpoint. Additionaly results
can also be discarded, this is useful for testing.

When you configure the crawler to **store results on disk**, it will save the results
to the local filesystem. The results are saved in the same directory as the crawl
request.

```sh
TOBEY_RESULTS_DSN=disk:///path/to/results
```

When you configure the crawler to **forward results to a webhook**, it will deliver
the results to a configured webhook endpoint. [Webhooks](https://mailchimp.com/en/marketing-glossary/webhook) are a technique to notify other services about a result, once its ready.

```sh
TOBEY_RESULTS_DSN=webhook://example.org/webhook
```

For the webhook method, **dynamic re-configuration** is supported. This means that you can
configure the webhook endpoint on a per-request basis. Dynamic re-configuration is disabled
by default, and can be enabled by adding `enable_dynamic_config` to the DSN.

```sh
TOBEY_RESULTS_DSN=webhook://example.org/webhook?enable_dynamic_config # with default endpoint
TOBEY_RESULTS_DSN=webhook://?enable_dynamic_config # without default endpoint, requires dynamic 
                                                   # rconfiguration in each crawl request
                                                   # request
```

You can than specify the webhook endpoint in the crawl request:

```jsonc 
{
  "url": "https://example.org",
  "results_dsn": "webhook://example.org/webhook"
}
```

When you configure the crawler to **discard results**, it will not store any results
by itself. This is useful for testing and **the default behavior**.

```sh
TOBEY_RESULTS_DSN=noop://
```

### Results Format

A _Result object_ is a JSON object that contains the result of a crawl request alongside
the metadata of the run, see _Runs_ above for more details.

```jsonc
{
  "action": "collector.response",
  "run_uuid": "0033085c-685b-432a-9aa4-0aca59cc3e12",
  "run_metadata": {
    "internal_project_reference": 42,
    "triggered_by": "user@example.org"
  },
  "request_url": "http://...", 
  "response_body": "...", // Base64 encoded raw response body received when downloading the resource.
  // ... 
}
```

