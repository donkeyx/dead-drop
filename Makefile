.PHONY: test tidy vectors generate-vectors

test:
	go test ./...

tidy:
	go mod tidy

# Regenerate blob golden vectors (committed under blob/testdata/).
generate-vectors:
	GENERATE_VECTORS=1 go test ./blob/ -run TestGenerateVectors -count=1

vectors: generate-vectors test
