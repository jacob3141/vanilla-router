# Container management compared with Portainer

The owner used Portainer only to manage Docker on the router, so the web interface should cover what Portainer does there. Functions are taken from
the [Portainer documentation](https://docs.portainer.io/user/docker) for a standalone Docker environment.

| Status | Meaning |
|---|---|
| done | in the web interface |
| reduced | the useful part exists, the limit is named |
| open | wanted, not built yet |
| no | does not apply to one router, or is left out on purpose |

## Containers

| Function | Status | Note |
|---|---|---|
| List, state, health, IP | done | |
| Start, stop, restart, kill, pause, resume, remove | done | |
| Rename | done | |
| Create: name, image, always pull, auto remove | done | |
| Create: command, entrypoint, working dir, user | done | |
| Create: logging driver and options | done | |
| Create: volumes and host paths, read only | done | Host paths outside the data folders need "Advanced" |
| Create: network, hostname, domain, MAC, IPv4, IPv6, DNS, hosts entries | done | |
| Create: environment, labels, restart policy | done | |
| Create: privileged, init, devices, sysctls, security options, shared memory | done | Behind "Advanced", except init and shared memory |
| Create: GPU | done | Behind "Advanced" |
| Create: memory limit and reservation, CPU limit | done | |
| Create: capabilities | done | Behind "Advanced" |
| Publish ports | no | Docker runs with `iptables: false` (D-20), so that the firewall stays ours. A container gets its own address instead |
| Details and inspect | done | |
| Edit, duplicate | done | Replaces the container, the old one is restored if that fails |
| Check for and apply a newer image | done | "Check for updates" asks the registries, "Update" downloads the image and recreates the container |
| Logs with search and download | done | |
| Statistics | done | Chart of CPU and memory, current network, disk and processes |
| Processes | done | |
| Console | done | A small terminal emulator, enough for shells and most full-screen programs |
| Attach | done | |
| Create an image from a container | done | |
| Join and leave networks | done | |
| Attach a volume | reduced | Through edit |
| Webhooks | done | Only from the home network, like the web interface |
| Ownership | no | One administrator |

## Stacks and templates

| Function | Status | Note |
|---|---|---|
| Add through the web editor or by upload | done | |
| Environment variables | done | |
| List, edit, redeploy, start, stop, remove, logs | done | |
| Show stacks made elsewhere | done | They can be started, stopped and removed, not edited |
| Custom templates | done | |
| Application templates | done | The community catalog of Portainer, fetched when the page opens |
| Add from a Git repository, GitOps updates | done | Over https, by polling. A push from Git needs the webhook |
| Webhooks | done | |
| Migrate, duplicate | reduced | Save as template, deploy under another name |

## Images

| Function | Status | Note |
|---|---|---|
| List, inspect, remove | done | |
| Pull, tag, push | done | |
| Build from a Dockerfile | done | Text only, no upload of a build context |
| Import, export | done | As a file in a data folder, which the file share can reach |
| Prune | done | |

## Networks and volumes

| Function | Status | Note |
|---|---|---|
| Networks: ipvlan L2 and L3, macvlan, bridge | done | A bridge network has no way out, because Docker sets no firewall rules |
| Networks: IPv4, IPv6, internal, labels, remove | done | |
| Networks: overlay | no | Swarm |
| Volumes: list, unused, add, remove, prune | done | |
| Volumes: NFS and CIFS | done | |
| Volumes: browse | done | Read only |

## The rest

| Function | Status | Note |
|---|---|---|
| Dashboard with counts | done | |
| Events | done | The last 24 hours |
| Host details and disk use | done | |
| Registries: log in and out | done | |
| Registries: browse, manage | no | Business Edition |
| Services, configs, secrets, Swarm | no | Swarm |
| Kubernetes, Helm | no | One Docker host |
| Environments, Edge agents | no | One Docker host |
| Users, teams, roles | no | One administrator |
