.PHONY: all frontend gateway orchestrator check clean

all: frontend gateway orchestrator

frontend:
	cd frontend && npm install && npm run build

gateway:
	mkdir -p bin
	cd gateway && go build -trimpath -ldflags='-s -w' -o ../bin/webnix-gateway .

orchestrator:
	mkdir -p bin
	cd orchestrator && go build -trimpath -ldflags='-s -w' -o ../bin/webnix-orchestrator .

check:
	./scripts/verify.sh

clean:
	rm -rf bin frontend/dist
