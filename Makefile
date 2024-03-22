.PHONY: dev
dev:
	TOBEY_DEBUG=true go run .

.PHONY: test
test:
	TOBEY_SKIP_CACHE=true TOBEY_DEBUG=true go test

.PHONY: clean
clean:
	if [[ -f tobey ]]; then rm tobey; fi
	if [[ -d cache ]]; then rm -r cache; fi
