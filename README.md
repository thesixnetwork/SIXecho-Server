# sixecho-server anatomy

```bash
.
├── db                        <-- This migration file genenate by alembic
├── sam                       <-- This is clouformation template work on aws platform
├── README.md                 <-- This instructions file
```

## Deploy Project
go to sam/api/v1.0 folder and doing by step below.
```bash
_deploy sixechoAPIv100 dev
```
And then please go to the cloud9 on aws console, open your project or create project IDE on cloud9 and clone this project from github. 
To use this command on console cloud9.
```bash
# zip file
cd sixecho-server/sam/api/v1.0/digest_checker
zip -r ../myDeploymentPackage.zip .
# deploy function
cd ..
aws lambda update-function-code --function-name {{function_name}} --zip-file fileb://myDeploymentPackage.zip
```
