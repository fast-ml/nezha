FROM centos:7

COPY webhook /webhook
RUN chmod +x /webhook
ENTRYPOINT ["/webhook"]
