FROM golang:1.24.2

WORKDIR /app

COPY . .

RUN go mod tidy
RUN go build -o gimnasios .

EXPOSE 50052

CMD ["./gimnasios"]