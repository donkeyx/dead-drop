# dead-drop Helm chart

This chart deploys stateless `dead-drop` replicas backed by an external PostgreSQL database.

Create the database URL Secret separately:

```bash
kubectl create secret generic dead-drop-db \
  --from-literal=database-url='postgres://user:password@postgres.example/deaddrop?sslmode=require'
```

Install with the Secret reference:

```bash
helm upgrade --install dead-drop ./deploy/helm/dead-drop \
  --set database.existingSecret=dead-drop-db
```

Set `autoscaling.enabled=true` to enable HPA. PostgreSQL is required for multiple replicas; SQLite and filesystem storage are not supported by this chart. The application rate limiter remains per pod until a shared limiter is added.
