.PHONY: install desktop engine test support-doc-check package dmg linux windows clean

install:
	npm install
	cd engine && go mod download

desktop:
	npm --workspace apps/desktop run start

engine:
	cd engine && go build -o bin/piper-engine ./cmd/daemon

test:
	cd engine && go test ./...
	cd engine && go run ./cmd/supportdoc -check
	npm --workspace packages/shared-types run build
	npm --workspace apps/desktop run typecheck

support-doc-check:
	cd engine && go run ./cmd/supportdoc -check

package:
	node scripts/make-desktop.mjs

dmg:
	node scripts/make-desktop.mjs darwin

linux:
	node scripts/make-desktop.mjs linux x64

windows:
	node scripts/make-desktop.mjs win32 x64

clean:
	rm -rf apps/desktop/.vite apps/desktop/out apps/desktop/node_modules packages/shared-types/dist node_modules engine/bin
