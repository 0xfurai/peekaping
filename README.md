# Peekaping - the best uptime kuma alternative

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-%23007d9c.svg?style=flat&logo=go&logoColor=white)
![React](https://img.shields.io/badge/react-%2320232a.svg?style=flat&logo=react&logoColor=%2361dafb)
![TypeScript](https://img.shields.io/badge/typescript-%23007acc.svg?style=flat&logo=typescript&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/postgresql-%23336791.svg?style=flat&logo=postgresql&logoColor=white)
![SQLite](https://img.shields.io/badge/sqlite-%2307405e.svg?style=flat&logo=sqlite&logoColor=white)
![Docker Pulls](https://img.shields.io/docker/pulls/0xfurai/peekaping-web)

**A modern, self-hosted uptime monitoring solution**

Peekaping is a uptime monitoring system built with Golang and React. You can monitor your websites, API and many more leveraging beautiful status pages, alert notifications.

🔗 Website **[peekaping.com](https://peekaping.com)**

🔗 Live Demo **[demo.peekaping.com](https://demo.peekaping.com)**

🔗 Documentation **[docs.peekaping.com](https://docs.peekaping.com)**

🔗 Community terraform provider **[registry.terraform.io/providers/tafaust/peekaping](https://registry.terraform.io/providers/tafaust/peekaping/latest)**

## Why Peekaping Is the Optimal Alternative to Uptime Kuma

Peekaping is a modern uptime monitoring solution designed with the requirements of professional DevOps teams in mind, addressing the key limitations of traditional monitoring systems.

**Key Advantages:**
- **API-first architecture** — all system functions are accessible through a RESTful API, ensuring complete automation and seamless integration with CI/CD processes and Infrastructure as Code tools
- **Easily extensible server architecture** — the modular structure allows adding new monitor types and notification channels without modifying the system core
- **Server built with Golang** — using one of the most performant compiled languages ensures high speed with minimal consumption of RAM and CPU resources
- **Unmatched stability** — thanks to a typed client and compiled Golang language, the system demonstrates high reliability and predictable operation
Modern and intuitive interface — clean user interface design built on contemporary UI/UX principles
- **Flexible storage options** — support for two popular databases (SQLite / PostgreSQL) allows adapting the solution to any infrastructure
- **API key management and access control** — built-in security system with access rights management and API keys provides enterprise-level protection


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

Then open http://localhost:8383. The image runs Peekaping in single mode: one process, as a non-root user, with no Redis. It serves the web app and the API on port 8383. For HTTPS, put a reverse proxy in front of it.

[Docker + SQLite Setup](https://docs.peekaping.com/self-hosting/docker-with-sqlite)

Peekaping also supports [PostgreSQL Setup](https://docs.peekaping.com/self-hosting/docker-with-postgres). Read docs for more guidance

These images run single mode, the default for new installs. Cluster mode (Kubernetes with Helm, or Docker Compose) is for installs that need to scale out. See [Deployment modes](https://docs.peekaping.com/self-hosting/deployment-modes) to choose.

MongoDB storage was removed. If you ran Peekaping on MongoDB, [move your data to SQLite or PostgreSQL](https://docs.peekaping.com/self-hosting/docker-with-mongo#move-to-sqlite-or-postgresql) before you upgrade.

## ☁️ One-Click Deploy

[![Deploy on RepoCloud](https://d16t0pc4846x52.cloudfront.net/deploylobe.svg)](https://repocloud.io/details/peekaping/)

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
- Microsoft Teams
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

## 💡 Motivation Behind Creating an Uptime Kuma Alternative

The creation of Peekaping was inspired by our experience using Uptime Kuma — a popular open-source monitoring solution. We deeply respect this project and its contribution to the community, but we aimed to address the systemic limitations that teams face when scaling and integrating monitoring into modern DevOps processes.

Our goal is to build a new system that combines the features requested by the community with modern technological approaches: strict typing and extensible architecture.

**Our Approach:**
**API as the foundation.** We designed Peekaping from the ground up as an API-first solution, where every function is accessible programmatically. This opens up possibilities for complete automation and integration with any tools.

**Performance through the right technology choices.** The server side is implemented in Golang — a fast and efficient language that delivers high performance with minimal RAM consumption. This is especially critical when monitoring a large number of services.

**Extensibility by design.** The system architecture allows easy addition of new notification channels, monitor types, and integrations without needing to modify the core codebase.

**Reliable client side.** The frontend is built with React and TypeScript, ensuring not only high performance but also reliability thanks to static typing. The client side was also designed with ease of extension in mind.

Peekaping is the ideal choice for teams that need a reliable and customizable uptime monitoring solution capable of growing alongside their infrastructure.


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
2. Set up your environment with the [development guide](https://docs.peekaping.com/development). You need Go, Node.js and Bun, installed any way you like; `make setup` checks them.
3. Create a feature branch
4. Make your changes
5. Add tests if applicable
6. Submit a pull request

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

## Criteria to Consider When Looking for Uptime Kuma Alternatives
When choosing an Uptime Kuma alternative, it's important to consider the technology stack and architecture. Solutions built with compiled languages (Go, Rust) deliver better performance and consume fewer resources. An API-first approach is critical for automation and integration with CI/CD processes.

Ensure the system supports the necessary monitoring protocols (HTTP/HTTPS, TCP, databases, Docker, gRPC) and integrates with your communication channels. Also important are flexible storage options (SQLite/PostgreSQL), security features (MFA, RBAC, API keys), modular architecture for extensibility, and an active community with quality documentation.

## How to Choose the Right Uptime Kuma Alternative
Determine the deployment format based on your capabilities and requirements. Self-hosted solutions provide complete control over data and deep customization options, but require technical resources for deployment and maintenance. Cloud solutions offer quick setup and automatic updates, but come with customization limitations and monthly subscriptions.

Conduct practical testing: deploy the system, create real monitors of different types, simulate failures, and evaluate detection speed. Check resource consumption under load, ease of API integrations, and interface quality. Teams choose Peekaping when they need high performance, API-first architecture, and freedom from vendor lock-in.
