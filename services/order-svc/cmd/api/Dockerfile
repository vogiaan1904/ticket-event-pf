# syntax=docker/dockerfile:1

##
## STEP 1 - BUILD
##

# specify the base image to  be used for the application, alpine or ubuntu
FROM golang:1.25-alpine AS builder

# create a working directory inside the image
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# download all dependencies
RUN go mod download

# Copy the source code into the container
COPY . .

# compile application
# /api: directory stores binaries file
RUN go build -o api ./cmd/api

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

# Install ca-certificates and timezone
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

# Set the timezone.
ENV TZ=UTC
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

# Copy the binary from the builder stage
WORKDIR /app
COPY --from=builder /app/api .

# Expose port 80
EXPOSE 80

# Run the application as orderapp user
USER orderapp

# Run the application
ENTRYPOINT ["./api"]