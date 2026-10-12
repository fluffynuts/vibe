// Forwards the sandbox's own address on 5391 to diffity, which listens on
// the loopback only. sbx publishes a port by connecting to the sandbox's
// network address, so without this the published port refuses connections.
// Listening on that address alone, not 0.0.0.0, leaves 127.0.0.1:5391 free
// for diffity itself, whichever of the two starts first.
'use strict';
const net = require('net');
const os = require('os');

const PORT = 5391;

function externalAddress() {
  for (const addrs of Object.values(os.networkInterfaces())) {
    for (const a of addrs || []) {
      if (a.family === 'IPv4' && !a.internal) return a.address;
    }
  }
  return null;
}

const host = externalAddress();
if (!host) {
  console.error('no network address to forward from');
  process.exit(1);
}

net
  .createServer((client) => {
    const upstream = net.connect(PORT, '127.0.0.1');
    client.on('error', () => upstream.destroy());
    upstream.on('error', () => client.destroy());
    client.pipe(upstream);
    upstream.pipe(client);
  })
  .on('error', (err) => {
    console.error('cannot listen on ' + host + ':' + PORT + ': ' + err.message);
    process.exit(1);
  })
  .listen(PORT, host, () => console.log('forwarding ' + host + ':' + PORT + ' to 127.0.0.1:' + PORT));
