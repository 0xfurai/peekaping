export type FaqItem = {
  question: string;
  answer: string;
};

export const faqItems: FaqItem[] = [
  {
    question: "What is Peekaping?",
    answer:
      "Peekaping is an open-source, self-hosted uptime monitoring and status page tool built with Go and React. It monitors websites, APIs, and internal services and sends real-time notifications when issues occur.",
  },
  {
    question: "How does Peekaping compare to Uptime Kuma?",
    answer:
      "Peekaping offers a similar experience with a focus on strongly typed code (Go + TypeScript), an API-first design with Swagger, and a modular architecture that makes it easy to extend and swap storage backends.",
  },
  {
    question: "Does Peekaping have public status pages?",
    answer:
      "Yes. You can publish branded public status pages that show uptime, and performance metrics.",
  },
  {
    question: "How do I deploy Peekaping?",
    answer:
      "Use official Docker images and docker-compose for quick setup, or run the Go API and React web app on a VM or bare metal.",
  },
  {
    question: "Which databases are supported?",
    answer:
      "Peekaping supports MongoDB with options for PostgreSQL and SQLite via its pluggable storage design.",
  },
  {
    question: "Is there a REST API?",
    answer:
      "Yes. Peekaping is API-first and includes Swagger/OpenAPI documentation for automation and integrations.",
  },
  {
    question: "Can I migrate from Uptime Kuma?",
    answer:
      "A migration tool is being developed. For now, you can migrate manually.",
  },
  {
    question: "Is Peekaping free for commercial use?",
    answer:
      "Yes. It’s MIT-licensed and free for personal and commercial projects.",
  },
];

export default faqItems;


