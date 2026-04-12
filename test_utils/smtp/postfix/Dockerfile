FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y postfix sasl2-bin libsasl2-modules openssl && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create directories
RUN mkdir -p /etc/postfix/sasl /etc/postfix/certs /var/spool/postfix/etc

# Copy configuration files
COPY main.cf /etc/postfix/main.cf
COPY master.cf /etc/postfix/master.cf
COPY sasl/smtpd.conf /etc/postfix/sasl/smtpd.conf

# Copy entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 25 587 465

ENTRYPOINT ["/entrypoint.sh"]
CMD ["sh", "-c", "postfix start && tail -f /var/log/postfix.log 2>/dev/null || tail -f /var/log/mail.log 2>/dev/null || sleep infinity"]

