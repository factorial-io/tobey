.PHONY: dev
dev:
	TOBEY_DEBUG=true go run .

.PHONY: clean
clean:
	if [[ -f tobey ]]; then rm tobey; fi
	if [[ -d cache ]]; then rm -r cache; fi
