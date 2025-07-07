const express = require("express");
const app = express();
const port = 3022;

const randomInt = (min, max) => {
  return Math.floor(Math.random() * (max - min + 1)) + min;
};


const simpleLogger = (req, res, next) => {
  const start = performance.now();

  res.on('finish', () => {
    const ms = (performance.now() - start).toFixed(1);
    console.log(
      `${req.method} ${req.originalUrl} → ${res.statusCode} (${ms} ms)`
    );
  });

  next();
}

app.use(express.json());
app.use(express.urlencoded({ extended: true }));
app.use(simpleLogger);

const simpleHandler = (req, res, next) => {
  try {
    const delay = parseInt(req.query.delay, 10) || 100;
    const delayRandom = parseInt(req.query.delayRandom, 10) || 20;
    const finalDelay = delay + randomInt(0, delayRandom);

    setTimeout(() => {
      res.send({
        status: "ok",
        delay: finalDelay,
      });
    }, delay);
  } catch (err) {
    next(err);
  }
}

// Redirect handler for testing max_redirects functionality
const redirectHandler = (req, res, next) => {
  try {
    const redirects = parseInt(req.query.redirects, 10) || 3;
    const current = parseInt(req.query.current, 10) || 0;
    const statusCode = parseInt(req.query.status, 10) || 302;
    const delay = parseInt(req.query.delay, 10) || 0;

    // Valid redirect status codes
    const validStatusCodes = [301, 302, 303, 307, 308];
    const finalStatusCode = validStatusCodes.includes(statusCode) ? statusCode : 302;

    const finalDelay = delay > 0 ? delay : 0;

    setTimeout(() => {
      if (current < redirects) {
        const nextUrl = `${req.protocol}://${req.get('host')}/redirect?redirects=${redirects}&current=${current + 1}&status=${finalStatusCode}&delay=${delay}`;
        console.log(`Redirecting (${current + 1}/${redirects}) to: ${nextUrl}`);
        res.redirect(finalStatusCode, nextUrl);
      } else {
        // Final destination reached
        res.send({
          status: "ok",
          message: "Redirect chain completed successfully",
          totalRedirects: redirects,
          finalDestination: true,
          redirectType: finalStatusCode
        });
      }
    }, finalDelay);
  } catch (err) {
    next(err);
  }
}

// Infinite redirect handler for testing redirect limits
const infiniteRedirectHandler = (req, res, next) => {
  try {
    const current = parseInt(req.query.current, 10) || 0;
    const statusCode = parseInt(req.query.status, 10) || 302;
    const delay = parseInt(req.query.delay, 10) || 0;

    const validStatusCodes = [301, 302, 303, 307, 308];
    const finalStatusCode = validStatusCodes.includes(statusCode) ? statusCode : 302;

    setTimeout(() => {
      const nextUrl = `${req.protocol}://${req.get('host')}/infinite-redirect?current=${current + 1}&status=${finalStatusCode}&delay=${delay}`;
      console.log(`Infinite redirect (${current + 1}) to: ${nextUrl}`);
      res.redirect(finalStatusCode, nextUrl);
    }, delay);
  } catch (err) {
    next(err);
  }
}

// Circular redirect handler for testing redirect loops
const circularRedirectHandler = (req, res, next) => {
  try {
    const path = req.path;
    const delay = parseInt(req.query.delay, 10) || 0;
    const statusCode = parseInt(req.query.status, 10) || 302;

    const validStatusCodes = [301, 302, 303, 307, 308];
    const finalStatusCode = validStatusCodes.includes(statusCode) ? statusCode : 302;

    setTimeout(() => {
      if (path === '/circular-redirect-a') {
        const nextUrl = `${req.protocol}://${req.get('host')}/circular-redirect-b?status=${finalStatusCode}&delay=${delay}`;
        console.log(`Circular redirect A → B: ${nextUrl}`);
        res.redirect(finalStatusCode, nextUrl);
      } else if (path === '/circular-redirect-b') {
        const nextUrl = `${req.protocol}://${req.get('host')}/circular-redirect-a?status=${finalStatusCode}&delay=${delay}`;
        console.log(`Circular redirect B → A: ${nextUrl}`);
        res.redirect(finalStatusCode, nextUrl);
      }
    }, delay);
  } catch (err) {
    next(err);
  }
}

// Basic endpoints
app.get("/", simpleHandler);
app.post("/", simpleHandler);
app.put("/", simpleHandler);
app.delete("/", simpleHandler);
app.patch("/", simpleHandler);

// Redirect testing endpoints
app.get("/redirect", redirectHandler);
app.post("/redirect", redirectHandler);
app.put("/redirect", redirectHandler);
app.delete("/redirect", redirectHandler);
app.patch("/redirect", redirectHandler);

// Infinite redirect testing
app.get("/infinite-redirect", infiniteRedirectHandler);
app.post("/infinite-redirect", infiniteRedirectHandler);

// Circular redirect testing
app.get("/circular-redirect-a", circularRedirectHandler);
app.get("/circular-redirect-b", circularRedirectHandler);

// Help endpoint
app.get("/help", (req, res) => {
  res.json({
    endpoints: {
      "/": "Basic endpoint with optional ?delay parameter",
      "/redirect": "Redirect testing endpoint",
      "/infinite-redirect": "Infinite redirect testing",
      "/circular-redirect-a": "Circular redirect testing (A→B→A...)",
      "/circular-redirect-b": "Circular redirect testing (B→A→B...)",
      "/help": "This help page"
    },
    redirectParameters: {
      "redirects": "Number of redirects before final response (default: 3)",
      "status": "HTTP redirect status code: 301, 302, 303, 307, 308 (default: 302)",
      "delay": "Delay in milliseconds between redirects (default: 0)"
    },
    examples: {
      "3 redirects": "http://localhost:3022/redirect?redirects=3",
      "5 redirects with 301": "http://localhost:3022/redirect?redirects=5&status=301",
      "10 redirects with delay": "http://localhost:3022/redirect?redirects=10&delay=100",
      "Infinite redirects": "http://localhost:3022/infinite-redirect",
      "Circular redirects": "http://localhost:3022/circular-redirect-a"
    },
    usageNotes: [
      "Use /redirect to test specific redirect counts",
      "Use /infinite-redirect to test redirect limits",
      "Use /circular-redirect-a or /circular-redirect-b to test redirect loops",
      "Monitor your PeekaPing max_redirects setting when testing"
    ]
  });
});

// Global error handler
app.use((err, req, res, next) => {
  console.error('Error:', err);
  res.status(500).json({ error: 'Internal Server Error', details: err.message });
});

// Process-level error handlers
process.on('uncaughtException', (err) => {
  console.error('Uncaught Exception:', err);
});

process.on('unhandledRejection', (reason, promise) => {
  console.error('Unhandled Rejection at:', promise, 'reason:', reason);
});

app.listen(port, () => {
  console.log(`Fake server listening on port ${port}`);
  console.log(`Visit http://localhost:${port}/help for redirect testing endpoints`);
});
