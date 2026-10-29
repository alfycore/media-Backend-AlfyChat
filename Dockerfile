FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/service .
RUN mkdir -p /out/uploads/avatars /out/uploads/banners /out/uploads/attachments /out/uploads/icons /out/uploads/wallpapers

# Image racine (comme l'ancienne image bun) : le volume media_uploads monté sur
# /app/uploads garde ses droits actuels.
FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=build /out/service /app/service
COPY --from=build /out/uploads /app/uploads
COPY public /app/public
EXPOSE 3007
ENTRYPOINT ["/app/service"]
