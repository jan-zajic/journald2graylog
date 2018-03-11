FROM fedora:latest

ADD ./journald2graylog /bin/journald2graylog
ADD ./docker/start.sh /bin/start.sh

CMD ["/bin/start.sh"]
