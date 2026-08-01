# One command to a runnable artifact:
#
#   docker build -t cron-parser-go .
#   docker run --rm cron-parser-go next "*/15 9-17 * * 1-5" -n 5
#
# The build stage compiles and runs the port's own Go tests, so an image that
# builds is an image whose tests passed. Running the original TypeScript suite
# needs the upstream repository alongside this one, which a container cannot
# reach, so that runs on the host with `npm test`.

FROM golang:1.24-alpine AS build

WORKDIR /src

# Dependencies first, so the module graph is cached separately from the source.
COPY go.mod ./
RUN go mod download

COPY cron/ ./cron/
COPY cmd/ ./cmd/

# The tests run during the build rather than after it: a failure here fails the
# image instead of producing one that merely compiles.
RUN go vet ./cron/... ./cmd/... && go test ./cron/... -count=1

# CGO is off so the binary is static and can run on scratch. Symbol tables and
# DWARF are stripped because nothing debugs this image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cron-parser ./cmd/cron-parser


# scratch rather than alpine: the binary is static and the zone database is
# embedded in it, so there is nothing else the image needs.
FROM scratch

COPY --from=build /cron-parser /cron-parser

ENTRYPOINT ["/cron-parser"]
CMD ["--help"]
