# Peekaping - the best uptime kuma alternative

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-%23007d9c.svg?style=flat&logo=go&logoColor=white)
![React](https://img.shields.io/badge/react-%2320232a.svg?style=flat&logo=react&logoColor=%2361dafb)
![TypeScript](https://img.shields.io/badge/typescript-%23007acc.svg?style=flat&logo=typescript&logoColor=white)
![MongoDB](https://img.shields.io/badge/mongodb-4ea94b.svg?style=flat&logo=mongodb&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/postgresql-%23336791.svg?style=flat&logo=postgresql&logoColor=white)
![SQLite](https://img.shields.io/badge/sqlite-%2307405e.svg?style=flat&logo=sqlite&logoColor=white)
![Docker Pulls](https://img.shields.io/docker/pulls/0xfurai/peekaping-web)

**A modern, self-hosted uptime monitoring solution**

Peekaping is a uptime monitoring system built with Golang and React. You can monitor your websites, API and many more leveraging beautiful status pages, alert notifications.

🔗 Website **[peekaping.com](https://peekaping.com)**

🔗 Live Demo **[demo.peekaping.com](https://demo.peekaping.com)**

🔗 Documentation **[docs.peekaping.com](https://docs.peekaping.com)**

🔗 Community terraform provider **[registry.terraform.io/providers/tafaust/peekaping](https://registry.terraform.io/providers/tafaust/peekaping/latest)**

## Why Peekaping Is the Best Alternative to Uptime Kuma

- API first architecture
- easy to extend server architecture
- Server written in golang that make it fast and lightweight using minimum RAM and CPU
- high stability thanks to typed client and compiled golang
- clean and modern ui design
- flexible storage options: SQLite / PostgreSQL / MongoDB
- API keys management and access control

## ⚠️ Beta Status

**Peekaping is currently in beta and actively being developed.**
Please note:

- The software is still under active development
- Some features could be changed
- I recommend testing in non-production environments first
- Please report any issues you encounter - your feedback helps us improve!

Please try Peekaping and provide feedback, this is huge contribution for us! Let's make Peekaping production ready.

## Quick start (docker + SQLite)

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -v $(pwd)/.data/sqlite:/app/data \
  --name peekaping \
  0xfurai/peekaping-bundle-sqlite:latest
```

[Docker + SQLite Setup](https://docs.peekaping.com/self-hosting/docker-with-sqlite)

Peekaping also support [PostgreSQL Setup](https://docs.peekaping.com/self-hosting/docker-with-postgres) and [MongoDB Setup](https://docs.peekaping.com/self-hosting/docker-with-mongo). Read docs for more guidance

## ⚡ Features

### Available Monitors

- HTTP/HTTPS
- TCP
- Ping (ICMP)
- DNS
- Push (incoming webhook)
- Docker container
- gRPC
- SNMP
- PostgreSQL
- Microsoft SQL Server
- MongoDB
- Redis
- MySQL/MariaDB -
- MQTT Broker
- RabbitMQ
- Kafka Producer

### 🔔 Alert Channels

- Email (SMTP)
- Webhook
- Telegram
- Slack
- Google Chat
- Signal
- Mattermost
- Matrix
- Discord
- WeCom
- WhatsApp (WAHA)
- PagerDuty
- Opsgenie
- Grafana OnCall
- NTFY
- Gotify
- Pushover
- SendGrid
- Twilio
- LINE Messenger
- PagerTree
- Pushbullet

### ✨ Other

- Beautiful Status Pages
- SVG Status Badges
- Multi-Factor Authentication (MFA)
- Brute-Force Login Protection
- SSL Certificate Expiration Checks

## 💡 Motivation behind creating uptime kuma alternative

Peekaping was deeply inspired by Uptime Kuma. We tried to cover all the fundamental flaws of Uptime Kuma. Alternative was to build new system with features community love and request but using typed approaches applying extendable architecture. First of all - we are API first. Our server side written in Golang, fast and efficient language that works with minimal RAM. Architecture allows easily extend system adding new notification channels, monitor types etc.

Client side written with React and typescript that makes it reliable and fast. Client side is also easily extensible.

Peekaping an ideal choice for teams who need a reliable, customizable uptime monitoring solution.

![Peekaping Dashboard](./pictures/monitor.png)

## 📡 Stay in the Loop

I share quick tips, dev-logs, and behind-the-scenes updates on&nbsp;Twitter.
If you enjoy this project, come say hi &amp; follow along!

[![Follow me on X](https://img.shields.io/twitter/follow/your_handle?label=Follow&style=social)](https://x.com/0xfurai)

## 🚧 Development roadmap

### General

- [ ] Incidents
- [ ] Migration tool (from uptime kuma)
- [ ] Multi user, groups, access levels
- [ ] Group monitors
- [ ] Add support for Homepage widget (in progress)
- [ ] Gatus like conditions

### Monitors

- [ ] HTTPs keyword and JSON query
- [ ] Steam
- [ ] GameDig
- [ ] Playwrite

### Notification channels

- [ ] Microsoft Teams
- [ ] WhatsApp (Whapi)
- [ ] CallMeBot (WhatsApp, Telegram Call, Facebook Messanger)
- [ ] AliyunSMS (阿里云短信服务)
- [ ] DingDing (钉钉)
- [ ] ClickSend SMS
- [ ] Rocket.Chat

![Alt](https://repobeats.axiom.co/api/embed/747c845fe0118082b51a1ab2fc6f8a4edd73c016.svg "Repobeats analytics image")

## 🤝 Contributing

We welcome contributions! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Inspired by [Uptime Kuma](https://github.com/louislam/uptime-kuma)
- Built with amazing open-source technologies
- Thanks to all contributors and users

## 📞 Support

- **Issues**: Report bugs and request features via GitHub Issues

---

**Made with ❤️ by the Peekaping team**
