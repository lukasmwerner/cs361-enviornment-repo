FROM --platform=$BUILDPLATFORM golang:alpine AS build

RUN apk add git

WORKDIR /src/

COPY go.* /src/

RUN go mod download -x

COPY . /src

#Compiler Settings
ENV CGO_ENABLED=0

# for full parings check out https://go.dev/doc/install/source#environment
ENV GOOS=linux
# this will be the target cpu arch
# Can be amd64 arm64 386 ppc64
ENV GOARCH=amd64

RUN go build -o /out/app .

# if you need certificates use: alpine
# otherwise just use: scratch
FROM alpine AS run 

COPY --from=build /out/app /
COPY *.db /

# if needed
EXPOSE 80

ENTRYPOINT [ "/app" ]
