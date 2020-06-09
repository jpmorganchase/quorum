# Quorum HA Setup

Quorum architecture allows for HA setup end to end for heightened availability for various input output operations. Although this increases the footprint of each node, the cost offset is compensated by low to zero downtime & horizontal scalability. In this section we will go through the configuration and setup in detail:

**WARNING**: Below HA setup hasn't been extensively tested and more for testnet as such should not be considered suitable for production use (yet).

## Quorum Node Configuration Requirements:

- Two or more Quorum Nodes serve as one client node.
- The inbound RPC requests from clients will be load balanced to one of these Quorum nodes.
- These nodes will need to share same key for transaction signing and should have shared access to key store directory or key vaults.
- These nodes need to share the same private state i.e., they connect to same Tessera node(s). This is done by proxy running on each Quorum node listening on local ipc file and directing request to Tessera Q2T http.

## Tessera Node Configuration Requirements:

- Separate Proxy server to redirect/mirror requests to two or more Tessera nodes 
- Two or more Tessera Nodes serve as Privacy manager for Client Quorum node.
- These nodes share same public/private key pair (stored in password protected files or external vaults) and share same database.
- In the server config, the bindingAddress should be the local addresses (their real addresses), but 'advertisedAddress' (serverAddress) needs to be configured to be the proxy
- Add DB replication or mirroring for Tessera private data store and the JDBC connection string to include both Primary DB and DR DB connections to facilitate auto switchover on failure.


??? info "Quorum HA Setup 1"
    ![Quorum Tessera HA Mode](QT_HA_1.png)

??? info "Quorum Full HA Setup "
    ![Quorum Tessera Full HA Mode](QT_HA_2.png)

??? info "Tessera HA Setup "
    ![Tessera HA Mode](Tessera_HA.png)
  

## Example Setup using nginx Proxy setup

### Proxy Setup on both Quorum nodes
    ```c
    load_module /usr/lib/nginx/modules/ngx_stream_module.so;
    error_log /home/ubuntu/nginx-error.log;
    events { }
    http {

        # Quorum-to-Tessera http
        upstream q2t {
                server url1:port1;
                server url2:port2;
            }

        server {
                listen unix:/home/ubuntu/tm.ipc;
                location / {
                        # Below is added to avoid transaction failure if partyinfo gets out of sync.
                        proxy_next_upstream error timeout http_404 non_idempotent; 
                        proxy_pass http://q2t;
                }
        }
    }
    ```
 

### Proxy server setup

 
    load_module /usr/lib/nginx/modules/ngx_stream_module.so;

    error_log /home/ubuntu/nginx-error.log;
    events { }
    stream {
    #Quorum json-rpc
        upstream quorum {
                server url1:port1;
                server url2:port2 backup;
        }
        server {
                listen 22000;
                proxy_pass quorum;
        }
      }
    http {

        # Third-party server
        upstream thirdparty {
                server url1:port1;
                server url2:port2;
        }
        server {
                listen 9081;
                location / {
                        proxy_next_upstream error timeout http_404 non_idempotent;
                        proxy_pass http://thirdparty;
                }
        }
        # Peer-to-peer server
        upstream p2p {
                server url1:port1;
                server url2:port2;
        }
        upstream p2p-mirror {
                server url1:port1;
                server url2:port2 backup;
        }
        server {
                listen 9001;

                location /resend {
                        proxy_pass http://p2p/resend;
                }
                location /push {
                        proxy_pass http://p2p/push;
                }
                location /partyinfo {
                        mirror /partyinfo-mirror;
                        proxy_pass http://p2p-mirror/partyinfo;
                }
                location /partyinfo-mirror {
                        internal;
                        proxy_pass url2:port2/partyinfo;
                }
                location /partyinfo/validate {
                        proxy_pass http://p2p/partyinfo/validate;
                }
                location /upcheck {
                        proxy_pass http://p2p/upcheck;
      }}}



