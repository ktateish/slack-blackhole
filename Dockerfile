FROM scratch
COPY ./slack-blackhole /slack-blackhole
CMD ["/slack-blackhole"]
