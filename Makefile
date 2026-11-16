VERSION := $(shell python3 -c "import json;print(json.load(open('custom_components/savant/manifest.json'))['version'])")
BIN     := custom_components/savant/bin
LDFLAGS := -s -w -X main.version=$(VERSION)
# linux/arm/7 is the SHR-2000; darwin covers Pro hosts.
TARGETS := linux/arm/7 linux/arm64/ linux/amd64/ darwin/arm64/ darwin/amd64/
PYTHON  ?= python3

.PHONY: agent test test-agent test-integration zip clean

agent:
	@mkdir -p $(BIN)
	@for t in $(TARGETS); do \
		os=$${t%%/*}; rest=$${t#*/}; arch=$${rest%%/*}; arm=$${rest#*/}; \
		out=savant-ha-$$os-$$arch$${arm:+v$$arm}; \
		(cd agent && CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch GOARM=$$arm \
			go build -trimpath -ldflags "$(LDFLAGS)" -o ../$(BIN)/$$out ./cmd/savant-ha) || exit 1; \
		echo "  $(BIN)/$$out"; \
	done
	@echo $(VERSION) > $(BIN)/VERSION

test: test-agent test-integration

test-agent:
	cd agent && go vet ./... && go test -race ./...

test-integration:
	$(PYTHON) -m pytest tests -q

zip: agent
	@mkdir -p dist
	cd custom_components/savant && zip -qr ../../dist/savant.zip . -x '__pycache__/*'
	@echo "  dist/savant.zip ($(VERSION))"

clean:
	rm -rf $(BIN) dist
