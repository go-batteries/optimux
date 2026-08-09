# energon

Image post processor. The worker is triggered as a lambda, consuming events from s3 buckets events.
Using a set number of image sizes, uses vips to resize the images and upload it back to s3.
