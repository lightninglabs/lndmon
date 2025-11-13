# Docker Compose Setup Instructions for Collecting Data From `lnd` _AND_ `lndmon`

# Table of Contents
1. [Setup](#setup)
   1. [Requirements](#requirements)
   2. [Lnd](#lnd)
   3. [Lndmon](#basic-lndmon-and-docker-compose-setup)
2. [Usage](#usage)
    1. [Option 1: Nginx Proxy Usage](#option-1-nginx-proxy-usage-requires-domain-name)
    2. [Option 2: Local Usage](#option-2-local-usage)
    3. [Customizing Grafana Metrics](#customizing-grafana-metrics)

## Setup

### Requirements
* docker >= 18.09.6
* docker-compose >= 1.24

### Lnd

- You need to have an `lnd` node built with the `monitoring` tag up and running
  with ports exposed and reachable.
- Run lnd with
   * `prometheus.enable=true`
   * `prometheus.listen=<IP>:8989`
   * `rpclisten=<IP>`
   * `tlsextraip=<IP>`
      - Make sure you have the node's reachable IP defined in the cert.
      - You may need to delete the existing `tls.cert` and `tls.key` in your lnd
        data directory and restart lnd to regenerate the cert.
- Start `lnd` *before* `lndmon`.


Note: See README.md for some additional consideration about the options that
are set above.


### Basic `lndmon` and Docker Compose Setup

- Clone this repo
```
git clone https://github.com/lightninglabs/lndmon
cd lndmon/
```
- In the `.env` file in this repo
   * Fill in the `TLS_CERT_PATH` and `MACAROON_PATH` variables relative to the
     host filesystem to allow `lndmon` to connect to your `lnd` node.
      - By default, `.lnd` lives in your home directory.
   * Fill in the `LND_HOST` variable to match your lnd node's IP and port.
   * Ensure the other lnd variables are also up-to-date.
   * If you wish to run `lndmon` connecting to an `lnd` node on testnet or simnet:
      - modify the `LND_NETWORK` variable to match your desired network.
      - make sure the `MACAROON_PATH` matches the desired network as well.
- Edit `prometheus.yml` `lnd` `targets` section to match your node's IP.



## Usage
### Option 1: Nginx Proxy Usage (requires domain name)
If you want to enable the built-in nginx proxy feature in order to access your Prometheus and Grafana dashboards remotely, these are the steps:
1. In the `lndmon` repository, edit the `.env` file and fill in the email, FQDN, and (optionally) timezone fields.
2. Ensure ports 80 and 443 on your machine are exposed to the internet.
3. (Optional) Basic auth setup for your Prometheus dashboard:
   - Install `apache2-utils` package.
   - Run `htpasswd -c nginx/etc/.htpasswd <YOUR_USERNAME>` and follow the prompts to enter and confirm your desired password.
   - In `lndmon/nginx/etc/service.conf`, uncomment the lines indicated in the file to enable basic auth.
4. If you want to use your own TLS certs:
   - Uncomment the lines beginning with `SSL_`  in `.env` and fill in the paths to your cert files.
   - Uncomment the lines beginning with `- SSL_` in `docker-compose.nginx.yml`.
5. Start everything up
   - `docker-compose -f docker-compose.yml -f docker-compose.nginx.yml up`
      - Run this command within the `lndmon` repository directory
      - Unless you opted to use your own certs above, this will result in the automatic generation of TLS certificates through Let's Encrypt if they haven't been generated already, or their renewal if the current certs have expired. The certs will automatically renew when they expire.
6. Grafana is located at `https://<YOUR_DOMAIN>/grafana/`
7. Prometheus's expression browser is located at `https://<YOUR_DOMAIN>/prometheus/graph`.

### Option 2: Local Usage
1. Start everything up
   - `docker-compose up`
     * Run this command within the `lndmon` repository directory
     * If you get the error "transport: Error while dialing dial tcp 172.17.0.1:10009: i/o timeout", your docker interface may not have the default IP. Make sure your docker interface's IP matches the IP for `LND_HOST` in `.env` and the lnd target's IP in `prometheus.yml`.
2. Access Grafana dashboard: 
   - Get Grafana's IP:
      - `docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' lndmon_grafana_1`
   - Grafana's dashboard is located at `http://<GRAFANA_IP>:3000/`.
      - The default password for the admin user is also admin (you can change it after the first login).
3. Access Prometheus expression browser:
   - Get Prometheus's IP:
      -`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' lndmon_prometheus_1`
   - Prometheus's expression browser is located at `http://<PROMETHEUS_IP>:9090/graph`.


### Customizing Grafana Metrics
`lndmon`'s Grafana instance comes with a set of basic dashboards. Add additional dashboards by clicking the `+' sign on the left.
