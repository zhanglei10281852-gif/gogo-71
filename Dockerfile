# Multi-stage build for the DebrisLedger CLI.
#
# Stage one compiles a fully static binary with the module's own toolchain
# version and no network access: GOTOOLCHAIN=local forbids downloading another
# toolchain and GOPROXY=off forbids fetching modules. The project has no
# dependencies beyond the Go standard library, so both are satisfied.
#
# Stage two is scratch and holds nothing but that binary.
FROM golang:1.22 AS builder

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOPROXY=off \
    GOFLAGS=-mod=mod

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN go vet ./... \
 && go build -trimpath -ldflags "-s -w" -o /out/debrisledger ./cmd/debrisledger

FROM scratch

COPY --from=builder /out/debrisledger /debrisledger

ENTRYPOINT ["/debrisledger"]
