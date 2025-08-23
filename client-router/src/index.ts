import express from "express";
import axios from "axios";

const EDGES = (process.env.EDGES || "").split(",").map(s => s.trim()).filter(Boolean);
const PORT = Number(process.env.ROUTER_PORT || 3000);
const ENABLE_CORS = (process.env.ENABLE_CORS || "false").toLowerCase() === "true";

if (EDGES.length === 0) {
  console.error("EDGES env var required (comma-separated list of edge base URLs)");
  process.exit(1);
}

const app = express();

if (ENABLE_CORS) { // simple CORS support for testing 
  app.use((_req, res, next) => {
    res.header("Access-Control-Allow-Origin", "*");
    next();
  });
}

app.get("/healthz", (_req, res) => res.send("ok"));

app.get("/asset", async (req, res) => {
  const path = (req.query.path as string) || "";
  if (!path.startsWith("/")) {
    res.status(400).json({ error: "query ?path=/your/file.txt required (leading slash)" });
    return;
  }

  // probe each edge's /ping to find the fastest in real time
  const results = await Promise.allSettled(EDGES.map(async base => {
    const t0 = Date.now();
    await axios.get(`${base}/ping`, { timeout: 2000 });
    const rtt = Date.now() - t0;
    return { base, rtt };
  }));

  const candidates = results
    .filter(r => r.status === "fulfilled")
    .map(r => (r as PromiseFulfilledResult<{ base: string; rtt: number }>).value)
    .sort((a, b) => a.rtt - b.rtt);

  if (candidates.length === 0) {
    res.status(503).json({ error: "no edges reachable" });
    return;
  }

  const chosen = candidates[0].base;

  try {
    // fetch from chosen edge
    const resp = await axios.get(`${chosen}/assets${path}`, { responseType: "arraybuffer", timeout: 4000 });
    // pass through content-type and include selected node header
    res.setHeader("Content-Type", resp.headers["content-type"] || "application/octet-stream");
    res.setHeader("X-Selected-Edge", chosen);
    res.status(resp.status).send(Buffer.from(resp.data));
  } catch (e: any) {
    res.status(502).json({ error: "edge fetch failed", details: e?.message });
  }
});

app.listen(PORT, () => {
  console.log(`Client router listening on :${PORT}`);
  console.log(`Edges: ${EDGES.join(", ")}`);
  console.log(`Try: curl "http://localhost:${PORT}/asset?path=/hello.txt" -i`);
});
