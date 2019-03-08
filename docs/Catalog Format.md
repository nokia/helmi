# Helmi Catalog Format

By default, Helmi reads the service catalog from the folder `./catalog`. Each
`.yml` or `.yaml` within this folder is parsed as a service definition. Helmi
is also able to read a ZIP file with the same structure of the `catalog` folder,
each contained YAML file is treated as a service definition.

A service definition contains of three parts, separated by the `---` yaml
document delimiter. The first part is parsed verbatim during startup and
declares a single service. The second and third section are templated using
the [sprig](https://masterminds.github.io/sprig/) template language. The second
section defines the values for Helm and is evaluated every time a new service
instance is created. The third section is evaluated every time user credentials
are bound.

## Example:

```yaml
---
# Service description:
service:
  _id: 1cb15f19-9892-4545-9728-0edf7143e9ef
  _name: "myservice"
  description: "My Service as a Service"
  chart: stable/service
  chart-version: 1.0.0
  tags:
  - database
  - mysql
  metadata:
    displayName: "My service"
  plans:
  -
    _id: 8cca984c-6e73-4f8d-b590-23e70867ee00
    _name: free
    description: "Free tier"
    metadata:
      displayName: "Free tier for my service"
  -
    _id: a4ef9493-ed99-45fd-aa03-7247cde88506
    _name: dev
    description: "Development tier"
    metadata:
      billing: true
    schemas:
      service-instance:
        create:
          parameters:
            $schema: http://json-schema.org/draft-04/schema#
            type: object
            properties:
              billing-account:
                description: Billing account number used to charge use of shared fake server.
                type: string
        update:
          parameters:
            $schema: http://json-schema.org/draft-04/schema#
            type: object
            properties:
            billing-account:
              description: Billing account number used to charge use of shared fake server.
              type: string
      service-binding:
        create:
          parameters:
            $schema: http://json-schema.org/draft-04/schema#
            type: object
            properties:
              billing-account:
                description: Billing account number used to charge use of shared fake server.
                type: string
    chart-values:
      dev: true
      replicaCount: 1
      replicaMinAvailable: 0
      
---
# Helm values used when a service is created.
#   Available template variables: .Service, .Plan, .Release.Name, .Cluster, .Parameters, .Context
chart-values:
  username: "{{ generateUsername }}"
  http_proxy: "{{ env "HTTP_PROXY" }}"
  desc: "{{ .Plan.Description }}"
  systemId: "{{ .Context.systemId }}
  
---
# Credentials reported when a new binding is created:
#   Available template variables: .Service, .Plan, .Values, .Release, .Cluster
user-credentials:
  hostname: "{{ .Release.Name }}-cassandra.{{ .Release.Namespace }}.svc.cluster.local"
  port: "{{ .Services.Port "svcname" 8080 }}"
  username: "{{ .Values.username }}"
  password: "{{ .Values.password }}"
# Optional list of URIs to which Helmi must connect successfully before a service is reported ready:
#   Supported protocols: http, https, tcp, tls
health-checks:
  - "http://{{ .Services.Address "svcname" .Values.service.port }}"
```
