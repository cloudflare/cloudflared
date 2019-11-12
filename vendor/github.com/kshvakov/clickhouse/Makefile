test:
	go install -race -v
	go test -i -v
	go test -race -timeout 30s -v .

coverage:
	go test -coverprofile=coverage.out -v .
	go tool cover -html=coverage.out
