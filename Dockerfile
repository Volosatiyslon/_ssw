FROM golang:1.17 as build

#RUN apt install openssh git

RUN go env -w GOPRIVATE='provatrepo/*'
RUN git config --global url.'git@provatrepo:'.insteadOf 'https://provatrepo/'

ARG SSH_GITLAB_KEY
RUN mkdir -m 700 ~/.ssh \
  && ssh-keyscan provatrepo > ~/.ssh/known_hosts \
  && echo "$SSH_GITLAB_KEY" > ~/.ssh/id_rsa \
  && chmod 600 ~/.ssh/*

WORKDIR /go/src/app

COPY ./go.mod ./go.sum ./
RUN go mod download

COPY . .
RUN go build -o wrapper

FROM debian:stable-slim

RUN mkdir /app
WORKDIR /app

COPY --from=build /go/src/app/wrapper .
#.
