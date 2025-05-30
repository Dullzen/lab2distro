FROM golang:1.24.2

WORKDIR /app

COPY . .
COPY entrenadores.json .

RUN go mod tidy
RUN go build -o entrenador .

CMD ["./entrenador"]