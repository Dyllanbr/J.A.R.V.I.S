import net from "node:net";

const [host, rawPort] = process.argv.slice(2);
const port = Number(rawPort);

if (!host || !Number.isInteger(port) || port < 1 || port > 65_535) {
  console.error("Usage: node scripts/check-port.mjs <host> <port>");
  process.exit(2);
}

const server = net.createServer();

server.once("error", () => {
  console.error(`Smoke test address ${host}:${port} is already in use.`);
  process.exitCode = 1;
});

server.listen({ host, port, exclusive: true }, () => {
  server.close((error) => {
    if (error) {
      console.error(`Could not release smoke test address ${host}:${port}.`);
      process.exitCode = 1;
    }
  });
});
