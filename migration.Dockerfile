FROM migrate/migrate:v4.18.2

WORKDIR /app

COPY go.mod go.mod
COPY go.sum go.sum
COPY cmd cmd
COPY internal internal


ENV CGO_CFLAGS_ALLOW="-Xpreprocessor"
ENV AWS_REGION=us-east-1

RUN migrate -version

ENTRYPOINT ["migrate", "-path=/app/internal/migrations", "-database=$PG_URL"]

CMD [ "up" ]
