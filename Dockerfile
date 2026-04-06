FROM scratch
ARG TARGETPLATFORM=./build/dist
COPY $TARGETPLATFORM/tool-containers-mcp /
COPY /tools.yaml /etc/tool-containers-mcp/tools.yaml
ENTRYPOINT ["/tool-containers-mcp"]
