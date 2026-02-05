# 在根目录下执行，遍历所有子文件夹并执行合并
find . -maxdepth 1 -type d \( ! -name "." \) -exec sh -c "
    cd '{}' && [ -d .git ] && {
        echo 'Updating {}...';
        git checkout g;
        # 自动判断是 master 还是 main 并合并
        CURRENT_MAIN=\$(git branch -a | grep -E 'master|main' | head -n 1 | sed 's/.* //');
        git merge \$CURRENT_MAIN;
    }
" \;