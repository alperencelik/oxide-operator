# Quick start

1. Create an API token (`oxide auth login` or the console) and store it in a Secret, then point an
   `OxideConnection` at it:

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: oxide-token
     namespace: default
   stringData:
     token: oxide-token-...
   ---
   apiVersion: oxide.100vms.com/v1alpha1
   kind: OxideConnection
   metadata:
     name: oxide
   spec:
     host: https://oxide.sys.example.com
     tokenSecretRef:
       name: oxide-token
       namespace: default
   ```

   `kubectl get oxc` shows the silo and user once the token works.

2. Apply resources that reference the connection. See [config/samples](../config/samples):

   ```yaml
   apiVersion: oxide.100vms.com/v1alpha1
   kind: Instance
   metadata:
     name: web-0
   spec:
     connectionRef:
       name: oxide
     project: demo
     ncpus: 2
     memory: 4Gi
     bootDisk:
       size: 20Gi
       image: ubuntu-24.04
   ```
