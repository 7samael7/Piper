.PHONY: install desktop engine test clean

install:
	npm install
	cd engine && go mod download

desktop:
	npm --workspace apps/desktop run start

engine:
	cd engine && go build -o bin/pipeline-engine ./cmd/daemon

test:
	cd engine && go test ./...
	npm --workspace packages/shared-types run build
	npm --workspace apps/desktop run typecheck

clean:
	rm -rf apps/desktop/.vite apps/desktop/out apps/desktop/node_modules packages/shared-types/dist node_modules engine/bin
