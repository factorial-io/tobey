.PHONY: dev
dev:
	TOBEY_SKIP_CACHE=true TOBEY_DEBUG=true TOBEY_HOST=127.0.0.1 go run .

.PHONY: pulse
pulse:
	go run cmd/pulse/main.go

.PHONY: test
test:
	TOBEY_SKIP_CACHE=true TOBEY_DEBUG=true go test

.PHONY: clean
clean:
	if [[ -f tobey ]]; then rm tobey; fi
	if [[ -d cache ]]; then rm -r cache; fi
