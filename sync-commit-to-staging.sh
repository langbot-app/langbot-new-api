#!/bin/bash

set -e

# 获取当前分支名
current_branch=$(git symbolic-ref --short HEAD)

# 切换到 deploy/staging 分支
git checkout deploy/staging

# 变基到原分支
git rebase "$current_branch"

# 推送 deploy/staging 到远端
git push

# 切回原始分支
git checkout "$current_branch"
