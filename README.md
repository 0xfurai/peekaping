# Peekaping 🚀

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-%23007d9c.svg?style=flat&logo=go&logoColor=white)
![React](https://img.shields.io/badge/react-%2320232a.svg?style=flat&logo=react&logoColor=%2361dafb)
![TypeScript](https://img.shields.io/badge/typescript-%23007acc.svg?style=flat&logo=typescript&logoColor=white)
![MongoDB](https://img.shields.io/badge/mongodb-4ea94b.svg?style=flat&logo=mongodb&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/postgresql-%23336791.svg?style=flat&logo=postgresql&logoColor=white)
![SQLite](https://img.shields.io/badge/sqlite-%2307405e.svg?style=flat&logo=sqlite&logoColor=white)
![Docker Pulls](https://img.shields.io/docker/pulls/0xfurai/peekaping-web)


**A modern, self-hosted uptime monitoring solution**

Peekaping is a powerful, feature-rich uptime monitoring system similar to Uptime Kuma, built with Go and React. Monitor your websites, APIs, and services with real-time notifications, beautiful status pages, and comprehensive analytics.

##  Live Demo

Want to see Peekaping in action? Try our live demo:

🔗 **[demo.peekaping.com](https://demo.peekaping.com)**

## 📚 Documentation

For detailed setup instructions, configuration options, and guides:

🔗 **[docs.peekaping.com](https://docs.peekaping.com)**

## ⚠️ Beta Status

**Peekaping is currently in beta and actively being developed.** While I am excited to share this project with the community, please note that:

- The software is still undergoing testing and refinement
- Some features may be incomplete or subject to change
- I recommend testing in non-production environments first
- Please report any issues you encounter - your feedback helps us improve!

We encourage you to try Peekaping and provide feedback, but please use it at your own discretion. Your testing and feedback are invaluable to making Peekaping production-ready! 🚀

## Quick start (docker + SQLite)
```bash
docker run -d --rm --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -e ACCESS_TOKEN_SECRET_KEY=test_access_token_secret_key_16_characters_long \
  -e REFRESH_TOKEN_SECRET_KEY=test_refresh_token_secret_key_16_characters_long \
  -v $(pwd)/.data/sqlite:/app/data \
  0xfurai/peekaping-bundle-sqlite:latest
```
[Docker + SQLite Setup](https://docs.peekaping.com/self-hosting/docker-with-sqlite)

Peekaping also support [PostgreSQL Setup](https://docs.peekaping.com/self-hosting/docker-with-postgres) and [MongoDB Setup](https://docs.peekaping.com/self-hosting/docker-with-mongo). Read docs for more guidance

## 💡 Motivation

Peekaping is designed as a modern alternative to Uptime Kuma, built with a focus on **strongly typed architecture** and **extensibility**. Our server is written in Go, a fast and efficient language that enables a small footprint while maintaining high performance. The codebase is structured for easy extensibility, allowing developers to seamlessly add new notification channels, monitor types, and even swap out the database layer without major refactoring.

The client-side application is also strongly typed and built with modern React patterns, making it equally extensible and maintainable. This combination of type safety, performance, and modular design makes Peekaping an ideal choice for teams who need a reliable, customizable uptime monitoring solution.

![Peekaping Dashboard](./pictures/monitor.png)

## 📡 Stay in the Loop

I share quick tips, dev-logs, and behind-the-scenes updates on&nbsp;Twitter.
If you enjoy this project, come say hi &amp; follow along!

[![Follow me on X](https://img.shields.io/twitter/follow/your_handle?label=Follow&style=social)](https://x.com/0xfurai)

## Development roadmap

### General
- [ ] Login bruteforce protection
- [ ] Add ability to set custom domain for status pages
- [ ] Incidents
- [ ] Certificate expiration check
- [ ] Badges
- [ ] Multi user
- [ ] Add support for Homepage (in progress)

### Monitors
- [x] MQTT
- [x] RabbitMQ
- [x] Kafka Producer
- [ ] Microsoft SQL Server
- [x] PostgreSQL
- [x] MySQL/MariaDB
- [x] MongoDB
- [x] Redis

### Notification channels
- [x] Discord
- [ ] Microsoft Teams
- [ ] Twilio
- [ ] WhatsApp (WAHA)
- [ ] WhatsApp (Whapi)
- [ ] WeCom (企业微信群机器人)
- [ ] CallMeBot (WhatsApp, Telegram Call, Facebook Messanger)
- [ ] LINE Messenger
- [ ] LINE Notify
- [ ] SendGrid
- [ ] AliyunSMS (阿里云短信服务)
- [ ] DingDing (钉钉)
- [ ] Pushbullet
- [ ] ClickSend SMS
- [ ] PagerTree
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
