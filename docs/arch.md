### **Text-Based Architecture Diagram for CI/CD Pipeline with AWS CDK**  

Sample flow of the codebuild cicd   

---

```
+--------------------------+
|   Developer Pushes Code  |
|   (GitHub Repository)    |
+------------+-------------+
             |
             v
+--------------------------+
|  GitHub Actions CI       |
|  - Runs Tests            |
|  - Validates Build       |
|  - Triggers AWS Pipeline |
+------------+-------------+
             |
             v
+-----------------------------+
|  AWS CodePipeline           |
|  - Orchestrates CI/CD       |
|  - Pulls Source from GitHub |
+------------+----------------+
             |
             v
+--------------------------+
|  AWS CodeBuild           |
|  - Installs Dependencies |
|  - Runs Unit Tests       |
|  - Builds Artifacts      |
|  - Prepares for Deploy   |
+------------+-------------+
             |
             v
+-------------------------------+
|  Manual Approval (Optional)   |
|  - Required before deployment |
+------------+------------------+
             |
             v
+--------------------------+
|  Deployment Stage        |
|  - AWS Elastic Beanstalk |
|  - AWS Lambda            |
|  - AWS EC2               |
|  (Choose One)            |
+------------+-------------+
             |
             v
+--------------------------+
|  AWS CloudWatch Logs     |
|  - Monitors Pipeline     |
|  - Tracks Failures       |
+------------+-------------+
             |
             v
+----------------------------+
|  Rollback (On Failure)     |
|  - Previous Stable Version |
+----------------------------+
```

---

### **Breakdown of the Components:**  
1. **GitHub Actions:** Runs CI (tests, validation) before AWS deployment.  
2. **AWS CodePipeline:** Manages the overall deployment process.  
3. **AWS CodeBuild:** Builds and tests the application.  
4. **Manual Approval:** (Optional) Prevents bad releases from going live.  
5. **Deployment Targets:**
   - AWS Elastic Beanstalk (Managed web hosting)  
   - AWS Lambda (Serverless API)  
   - AWS EC2 (Custom server hosting)  
6. **CloudWatch Logs:** Monitors deployment pipeline and application health.  
7. **Rollback Mechanism:** Automatically reverts to the last stable version on failure.  

---

### **Key Workflow Actions:**  
1. **Developer pushes code** to GitHub → Triggers GitHub Actions.  
2. **GitHub Actions runs tests** and **triggers AWS CodePipeline** on success.  
3. **AWS CodePipeline fetches code** from GitHub and triggers CodeBuild.  
4. **AWS CodeBuild installs dependencies, runs tests, and builds artifacts.**  
5. **Manual approval (if enabled) is required before deploying.**  -> Might omit or make auto!
6. **Deployment to AWS Elastic Beanstalk, Lambda, or EC2.**  
7. **AWS CloudWatch logs the process** and tracks failures.  
8. **Rollback triggers if a deployment fails.**  

---

### **Scalability Considerations**  
- **Multi-Environment Support** → Extend for staging/production.  
- **Secrets Management** → Integrate AWS Secrets Manager.  
- **Monitoring & Alerts** → Add SNS for failure notifications.  

---
