   # Use the official Golang image as the base image
   FROM golang:1.25.5-alpine

   # Install necessary tools including make
   RUN apk add --no-cache make

   # Set the Current Working Directory inside the container
   WORKDIR /app

   # Copy the source from the current directory to the Working Directory inside the container
   COPY . .

   # Download all dependencies
   RUN go mod tidy
   RUN go mod download

   # Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
   RUN go mod download && \
    apk add --no-cache curl
 

   # Build the Go app
   RUN make all

   # Command to run the executable
   CMD ["./specmatic-order-bff-go"]