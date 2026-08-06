FROM gcr.io/distroless/static-debian12:nonroot
USER 8675:8675
COPY talstomize /
ENTRYPOINT ["/talstomize"]
