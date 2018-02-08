#!/usr/bin/env bash

DOC=true
MYPKG=sample
mkdir -p $MYPKG

MakeInterface() {
    struct=$1
    service=$2
    package=$3
    ifacemaker --file $HOME/go/src/$package \
               --struct ${struct} \
               --iface ${struct}Interface \
               --pkg ${MYPKG} \
               --add-import ${package} \
               --rewrite ${service} \
               --doc=${DOC} \
               --output ${MYPKG}/${service}.go
    return $?
}

#             struct  rewrite  go src path
MakeInterface Lambda  lambda   github.com/aws/aws-sdk-go/service/lambda
MakeInterface Route53 route53  github.com/aws/aws-sdk-go/service/route53
MakeInterface EC2     ec2      github.com/aws/aws-sdk-go/service/ec2
MakeInterface S3      s3       github.com/aws/aws-sdk-go/service/s3
