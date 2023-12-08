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
- Honors sitemap.xml and robots.txt

## Basic Usage

The following command will start the service, the service will bind to
0.0.0.0:8080 by default. You must have Go installed on your system.

```sh
go run .
```

```sh
curl -X POST http://127.0.0.1:8080/sites \
     -H 'Content-Type: application/json' \
     -d '{"url": "https://www.factorial.io/"}'
```

```json
{
    "url": "https://www.factorial.io",
    "meta": {
         
    }
}
```

