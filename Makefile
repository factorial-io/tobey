.PHONY: dev
dev:
	TOBEY_SKIP_CACHE=true TOBEY_DEBUG=true TOBEY_RESULT_REPORTER_DSN=disk:///tmp/tobey go run . -d -host 127.0.0.1

.PHONY: with-pulse
with-pulse:
	TOBEY_SKIP_CACHE=true TOBEY_DEBUG=true TOBEY_RESULT_REPORTER_DSN=disk:///tmp/tobey TOBEY_TELEMETRY=pulse go run . -d -host 127.0.0.1

.PHONY: pulse
pulse:
	go run cmd/pulse/main.go

.PHONY: pulse-test-data
pulse-test-data:
	for i in {1..10}; do curl -X POST http://127.0.0.1:8090/rps -d "$$i"; sleep 1; done

.PHONY: test
test:
	TOBEY_SKIP_CACHE=true go test -v ./...

.PHONY: clean
clean:
	if [[ -f tobey ]]; then rm tobey; fi
	if [[ -d cache ]]; then rm -r cache; fi
