FROM alpine:3.22
ARG LAB_VALUE=first
COPY marker.txt /opt/lewbox/marker.txt
RUN printf '%s\n' "$LAB_VALUE" > /opt/lewbox/build-arg.txt \
    && chown 1234:2345 /opt/lewbox/marker.txt \
    && chmod 0640 /opt/lewbox/marker.txt
CMD ["sh", "-c", "echo startup-ran > /tmp/lewbox-startup; exec sleep infinity"]
HEALTHCHECK CMD test -f /tmp/lewbox-startup
