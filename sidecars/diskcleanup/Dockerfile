FROM alpine:latest
RUN apk add --no-cache bash coreutils

COPY cleanup.sh /cleanup.sh
RUN chmod +x /cleanup.sh

CMD ["sh", "/cleanup.sh"]
