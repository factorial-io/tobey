#!/usr/bin/env node

const http = require("http");

// Define a server that simulates several kind of error conditions.

// On the /503 route it returns a 503 status code with a Retry-After header
// that tells the client to retry after 120 seconds.

// On the /slow route the server hangs the request for 120 seconds before
// responding with a 200 status code.

const requestHandler = (req, res) => {
  if (req.url === "/503") {
    res.writeHead(503, {
      "Retry-After": 120,
    });
    res.end("Service Unavailable");
  } else if (req.url === "/slow") {
    setTimeout(() => {
      res.writeHead(200);
      res.end("OK");
    }, 120000);
  } else {
    res.writeHead(404);
    res.end("Not Found");
  }
};

// Create the server
const server = http.createServer(requestHandler);

// Start the server on port 9090
const PORT = 9090;
server.listen(PORT, () => {
  console.log(`Server running on port ${PORT}`);
});
