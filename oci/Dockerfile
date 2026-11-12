FROM golang:latest
RUN git clone https://github.com/DazFather/Wednesday.git
WORKDIR ./Wednesday
RUN go install ./cmd/wed
WORKDIR ..
RUN rm -fr ./Wednesday
