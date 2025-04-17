# Tobey, a throughput optimizing web crawler

Tobey is a throughput optimizing but friendly web crawler, that is scalable from a single instance to a cluster. It features intelligent rate limiting, distributed coordination, and flexible deployment options. Tobey honors resource exclusions in a robots.txt and tries its best not to overwhelm a host.

## Quickstart

Start tobey as a service, and interact with it:

```sh
go run . # Start the service.
curl -X POST http://127.0.0.1:8080 -d 'https://www.example.org' # Submit a request.
```

## CLI Mode

Tobey offers an - albeit limited - cli mode that allows you to run ad hoc crawls. 

```sh
go build -o /usr/local/bin # Make tobey globally available as a command.
```

Target URLs can be provided via the `-u` flag. By default results
will be saved in the current directory. Use the `-o` flag to specify a different output directory. Use the `-i` flag to specify paths to ignore. For all remaining options, please review the cli help via `-h`. 

```sh
tobey -h
tobey -u https://example.org
tobey -u https://example.org/blog,https://example.org/values -o results
tobey -u https://example.org -i search/,admin/
```

## Submitting Crawl Requests

Tobey accepts crawl requests at the `/` endpoint. 

### Targets

 A target serves as the entrypoint of the crawling process. Using the entrypoint resources will be automatically discovered, by extracting links from scraped web pages and using a sitemap, if one is available. 

For each crawl request tobey requires at least one or multiple target URLs. These can be specified under the `url` and `urls` using JSON property accordingly:

```jsonc
{
  "url": "https://example.org"
}
```

```jsonc
{
  "urls": [
    "https://example.org/blog", 
    "https://example.org/values"
  ]
}
```

As a shorthand you can also provide targets as plaintext, multiple targets are specified
as a newline delimited list of URLs:

```
https://example.org
...
```

### Sitemaps

Sitemaps are along the provided target URL another entrypoint to discover more URLs to scrape. Tobey
will automatically discover sitemaps from well known locations. You can also provide them yourself
alongside the target URLs.

```jsonc
{
  "urls": [
    "https://example.org", 
    "https://example.org/i-am-also-a-sitemap.xml"
  ],
}
```

### Authentication

Access restricted targets, that require authentication can be crawled as well. Credentials can be provided along your crawl request using the `auth` property. The crawler will authenticate any resources using provided credentials for the same host of the target, it wont use it for other hosts.

_Note:_ Currently tobey supports only HTTP basic authentication. 

```jsonc
{
  "url": "https://example.org",
  "auth": [
    { "host": "example.org", "method": "basic", "username": "foo", "password": "secret" }
  ]
}
```

As a shorthand and for HTTP Basic Auth you can provide the credentials alongside a target URL as well:

```
https://foo:secret@example.org
```

### User Agents

If a certain website requires a specific user agent string, you can override the
default user agent (`Tobey/0`) for a specific run via the `ua` field in the crawl request:

```jsonc
{
  "url": "https://example.org",
  "ua": "CustomBot/1.0" // Overrides the default user agent for this run.
}
```

### Attaching Metadata

Arbitrary metadata can be provided along your crawl request. This metadata is internally
associated with the run that is created for your request and will be part of each
result coming from that run.

Submit metadata using the `metadata` option:

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

Find the metadata under the `run_metadata` key, when collecting results. Read more about the results format,
under _Collecting Results_ below.

```jsonc
{
  "run": "...",
  "run_metadata": { /* find your metadata here */ }
  // ... 
}
```

### Scope Control

By default crawling is constrained to the host as provided in the target URL. 
This is great if you don't want to end up crawling the whole internet, yet. If you want
to change this behavior and widen or tighten your crawl, read on.

#### Domain Aliases

By default only a target's domain is allowed. Sometimes a crawl target is available under several aliased domains. In this case you want to use the `aliases` property to add the aliased
domains as well:

```jsonc
{
  "url": "https://example.org",
  "aliases": [ 
    "example.com", // Entirely different domain, but same content.
  ]
}
```

_Note:_ It's enough to provide the "naked" version of the domain
without the "www" subdomain. Providing "example.org" implies "www.example.org"
as well as all other subdomains. Providing a domain with "www." prefix will also
allow the naked domain (and all its subdomains).

#### Ignore Paths

To skip resources with certain path segments, the `ignores` option can be used. This
is a gitignore-style list of paths or [match patterns](https://github.com/bmatcuk/doublestar?tab=readme-ov-file#patterns). 

In first example `/search` as well as `/en/search` and everything below would **not** be crawled. In the second example
only `/en` and everything below **will** be crawled.

```jsonc
{
  "url": "https://example.org",
  // ...
  "ignores": [
    "search/"
  ]
}
```

```jsonc
{
  "url": "https://example.org",
  // ...
  "ignores": [
    "/*" // First, ignore everything, turning the following into an allow list.
    "!/en" // Allow everything below /en
  ]
}
```

## Tracking Progress

Tobey can report progress while it's crawling. This is useful for monitoring the
progress of a crawl and for debugging and determine when a crawl has finished. By 
default this feature is disabled.

### Progress Reporters

```sh
TOBEY_PROGRESS_DSN=factorial://host:port # To report progress to the Factorial progress service.
TOBEY_PROGRESS_DSN=console # To report progress to the console.
```

Note: For HTTPS use the `factorials` scheme.

## Collecting Results

Once a crawl request is started being processed, and web pages scraped, results will become available as the processing continues. A result
usually contains the following data, its exact format depends a little bit on the chose result reporter, see below.

```jsonc
{
  "run": "0033085c-685b-432a-9aa4-0aca59cc3e12",
  "run_metadata": {
    "internal_project_reference": 42,
    "triggered_by": "user@example.org"
  },
  // "disovered_by": ["sitemap"] // TODO: How the resource was discovered, i.e. sitemap, robots, link.
  "request_url": "http://...", 
  "response_body": "...", // Base64 encoded raw response body received when downloading the resource.
  // ... 
}
```

### Result Reporters

Tobey currently supports multiple methods to handle results. You can either store
them locally on disk, forward them to a webhook endpoint, or store them in Amazon S3.

When you configure the crawler to **store results on disk**, it will save the results
to the local filesystem. By default the results are saved in the same directory as the crawl
request if not otherwise configured.

```sh
TOBEY_RESULT_REPORTER_DSN=disk:///path/to/results
```

When you configure the crawler to **store results in S3** (or compatible), it will save the results
to the specified bucket. The results will be organized in a directory structure
under the optional prefix:

```sh
TOBEY_RESULT_REPORTER_DSN=s3://bucket-name/optional/prefix
```


For a deployment example with S3 storage integration, see [`examples/s3/compose.yml`](examples/s3/compose.yml).

Note: When using S3 storage, make sure you have proper AWS credentials configured
either through environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) or
through AWS IAM roles.

[Webhooks](https://mailchimp.com/en/marketing-glossary/webhook) are a technique to notify other services about a result, once its ready. 
When you configure the crawler to **forward results to a webhook**, it will deliver the results to a configured webhook endpoint. 

```sh
TOBEY_RESULT_REPORTER_DSN=webhook://example.org/webhook
```

#### Dynamic Re-configuration

Tobey supports dynamic re-configuration of the result reporter at runtime. This
means that you can change the result reporter configuration while the crawler is
running. 

**Dynamic re-configuration is disabled by default, for security reasons. Enable only if you can trust the users that submit the crawl requests, i.e. if tobey is deployed as an internal service.**

```sh
TOBEY_DYNAMIC_CONFIG=yes
```

You can than use the `result_reporter_dsn` field in the crawl request to specify varying webhook endpoints:

```jsonc 
{
  "url": "https://example.org",
  "result_reporter_dsn": "webhook://example.org/webhook"
}
```

## Configuration

Using sane defaults, tobey works out of the box without additional configuration. If you want to configure it,
the following environment variables and/or command line flags can be used. Please also see `.env.example` for a working
example configuration.

| Variable / Flag   | Mode | Default  | Supported Values | Description                      |
|----------------|------|----------------|------------------|----------------------------------|
| `TOBEY_DEBUG`, `-debug` | Both | `false` | `true`, `false`  | Controls debug mode. |
| `TOBEY_SKIP_CACHE`, `-no-cache` | Both | `false` | `true`, `false`  | Controls caching access. |
| `TOBEY_UA`, `-ua` | Both | `Tobey/0`| any string | User-Agent to identify with. |
| `TOBEY_HOST`, `-host` | Service | empty | i.e. `localhost`, `127.0.0.1` | Adress to bind the HTTP server to. Empty means listen on all. |
| `TOBEY_PORT`, `-port` | Service | `8080` | `1-65535` | Port to bind the HTTP server to. |
| `TOBEY_WORKERS`, `-w`| Both | `5` | `1-128` | Number of workers to start. |
| `TOBEY_REDIS_DSN` | Service | empty | i.e. `redis://localhost:6379` | DSN to reach a Redis instance for coordinting multiple instances. |
| `TOBEY_PROGRESS_DSN` | Service | `noop://` | `memory://`, `factorial://host:port`, `console://`, `noop://` | DSN for progress reporting service. |
| `TOBEY_RESULT_REPORTER_DSN` | Service | `disk://results` | `disk:///path`, `webhook://host/path`, `noop://` | DSN specifying where crawl results should be stored. |
| `TOBEY_TELEMETRY`, `-telemetry` | Service | empty | `metrics`, `traces`, `pulse` | Space separated list of what kind of telemetry is emitted. |
| `-i` | CLI | empty | comma-separated paths, i.e. `/search`, `'*.pdf'`, `'*.(pdf\|asc)'` | Paths to ignore during crawling. |
| `-oc` | CLI | `false` | `true`, `false` | Store response bodies directly on disk without JSON wrapper. |

_Note:_ When enabling telemetry ensure you are also providing [OpenTelemetry environment variables](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/).

## Deployment Options

### Dependency Free

By default Tobey runs without any dependencies on any other service. In this mode
the service will not coordinate with other instances. It will store results locally 
on disk, but not report any progress. If you are trying out tobey this is the
easiest way to get started.

See [`examples/simple/compose.yml`](examples/simple/compose.yml) for a minimal deployment example.

### Stateless Operation

At Factorial Tobey is used by different services that need to crawl websites. The services 
collect the results on their side, or at a service that is part of their specific pipeline. 

Here tobey is configured to allow dynamic configuration, so individual services that submit
crawl requests, provide information on where the results should go.

```sh
TOBEY_DYNAMIC_CONFIG=yes
TOBEY_RESULT_REPORTER_DSN=
```

```jsonc
{
  "url": "https://example.org",
  "result_reporter_dsn": "webhook://my-internal-service/crawl-webhook"
}
```

### Pipelined Operation

At Factorial we use Tobey as part of a multi-stepped pipeline. Within these pipelines runs are performed 
and tracked. At the top of the pipeline a run UUID is generated by a service and subsequently passed 
along each step. For this reason tobey accepts a custom run UUID, when submitting a crawl request.

```jsonc
{
  "url": "...",
  "run": "0033085c-685b-432a-9aa4-0aca59cc3e12"
  // ...
}
```

The results of the run will carry the ID under the `run` or `run_uuid` property, depending on the chosen
result reporter.

```jsonc
{
  "run": "0033085c-685b-432a-9aa4-0aca59cc3e12",
  // ...
  "request_url": "http://...", 
  "response_body": "...",
  // ... 
}
```

See [`examples/pipelined/compose.yml`](examples/pipelined/compose.yml) for a deployment example with pipeline integration.

### Distributed Operation

The service is horizontally scalable by adding more instances on nodes
in a cluster. In horizontal scaling, any instances can receive crawl requests,
for easy load balancing. The instances will coordinate with each other via Redis.

```sh
TOBEY_REDIS_DSN=redis://localhost:6379
```

See [`examples/distributed/compose.yml`](examples/distributed/compose.yml) for a deployment example with Redis coordination.

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

## Telemetry

Tobey provides observability capabilities through multiple telemetry features that can be enabled via the `TOBEY_TELEMETRY` environment variable. For a complete deployment example with instrumentation and telemetry, see [`examples/instrumented/compose.yml`](examples/instrumented/compose.yml). 

```sh
TOBEY_TELEMETRY="metrics traces pulse"
```

- With **metrics** enabled, tobey will expose a Prometheus compatible endpoint for scraping and optionally submit OpenTelemetry metrics, if an endpoint is configured (see below).
- With **tracing** enabled, tobey will generate OpenTelemetry traces for the lifecyle of a crawl request.
- With **pulse** enabled, tobey provides real-time metrics, which can be monitored using the pulse tool via `go run cmd/pulse/main.go`

When using OpenTelemetry exporters, make sure to configure the appropriate endpoints:

```sh
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://jaeger:4318/v1/traces
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://otel-collector:4318/v1/metrics
```

## Limitations

Also Tobey can be configured - on a per run basis - to crawl websites behind
HTTP basic auth, **it does not support fetching personalized content**. It is
expected that the website is generally publicly available, and that the content
is the same for all users. When HTTP basic auth is used by the website it must
only be so in order to prevent early access.

Per-run user agent strings are only used when visiting a website, they are not
used, i.e. when forwarding results to a webhook.