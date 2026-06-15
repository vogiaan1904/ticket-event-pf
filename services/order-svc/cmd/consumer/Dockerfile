# syntax=docker/dockerfile:1

##
## STEP 1 - BUILD
##

# specify the base image to  be used for the application, alpine or ubuntu
FROM golang:1.25-alpine AS builder

# create a working directory inside the image
WORKDIR /app

# copy go mod and sum files
COPY go.mod go.sum ./

# download Go modules and dependencies
RUN go mod download

# copy the source code into the container
COPY . .

# compile application
# /api: directory stores binaries file
RUN go build -o consumer ./cmd/consumer

##
## STEP 2 - DEPLOY
##
FROM alpine:latest

# Set environment variables
ENV UID=2024
ENV USER=orderapp
ENV GID=2024
ENV GROUP=orderapp

# Create user
RUN CONFIG="\
    --user=$USER \
    --group=$GROUP \
    " && addgroup -g $GID -S $USER \
    && adduser -D -S -h /app -s /bin/bash -G $GROUP -u $UID $USER

RUN apk add --no-cache ca-certificates && update-ca-certificates

# Set the timezone.
ENV TZ=UTC
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

WORKDIR /app
COPY --from=builder /app/consumer .

EXPOSE 80

# Run the application as orderapp user
USER orderapp

ENTRYPOINT ["./consumer"]
