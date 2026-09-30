#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成可直接导入企业微信通讯录的成员信息表，外加一份组织架构说明表。

设计要点
--------
1. 产出两个工作簿：
   - 《企业微信通讯录导入-成员信息.xlsx》：**只有官方模板列**，行列结构不做任何增删改，
     拿去管理后台【通讯录 -> 批量导入/导出 -> 文件导入】直接上传即可。
   - 《企业微信组织架构与导入说明.xlsx》：给人看的，含组织架构总览、部门清单、
     成员花名册、校验报告、迭代日志与导入步骤（这个文件不要上传）。
2. 生成过程采用「规则校验 + 自动修复」的迭代循环（loop engineering）：
   每一轮先跑全量规则校验，收集违规；再按违规类别施加**最小影响**的修复；重复直到
   0 错误或达到轮次上限。中间每一轮的结果都写进迭代日志，可复盘。
3. 第 1 轮刻意保留一份「未经治理的原始花名册」（超长部门路径、拼音重名帐号、
   字段带半角逗号、部门缺负责人、姓名尾随空格、缺联系方式），用来演示循环的收敛过程。

官方规则出处（企业微信帮助中心 / 开发者中心，2026-09 核对）：
  - 必填：姓名、帐号、部门、手机或个人邮箱（二选一）；字段值不得含半角逗号「,」。
  - 帐号：1-32 个字母/数字/点/减号/下划线，帐号相同会覆盖导入。
  - 部门：上下级用「/」分隔且**从最上级部门开始**；多部门用「;」分隔，第一个为主部门；
    **部门字段长度不能超过 32 个字符**。
  - 部门最大层级 15 层；部门总数 ≤ 3 万；同级部门名称不可重复；名称不含 * ? " |。
  - 未验证/未认证企业人数上限默认 200 人。
"""

from __future__ import annotations

import re
from copy import deepcopy
from datetime import datetime
from pathlib import Path

from openpyxl import Workbook
from openpyxl.styles import Alignment, Border, Font, PatternFill, Side
from openpyxl.utils import get_column_letter

# --------------------------------------------------------------------------
# 0. 可调参数
# --------------------------------------------------------------------------

OUT_DIR = Path("/Users/zmk/Documents/vsc/golang/orderhub/outputs/wecom")

# 企业名。企业微信的根部门即企业本身，官方模板示例（"腾讯公司/微信事业群/广州研发部"）
# 也是从企业名开始写，所以这里作为部门路径的第一段。
COMPANY = "taskpilot怀民"

# 导入文件的表头。顺序与命名依据官方帮助文档的字段罗列：
# 「姓名、账号、别名、职务、部门、性别、手机、座机、个人邮箱」。
# 若管理后台提示列名不识别，把这一行替换成官方模板的首行即可（数据行无需改动）。
HEADERS = ["帐号", "姓名", "别名", "职务", "部门", "性别", "手机", "座机", "个人邮箱"]
REQUIRED_HEADERS = {"帐号", "姓名", "部门", "手机", "个人邮箱"}  # 手机与邮箱二选一

MAX_DEPT_FIELD_LEN = 32      # 部门字段（整条路径）字符上限
MAX_DEPT_DEPTH = 15          # 部门最大层级
MAX_MEMBERS = 200            # 未认证企业人数上限
ACCOUNT_RE = re.compile(r"^[A-Za-z0-9._-]{1,32}$")
MOBILE_RE = re.compile(r"^(\+[0-9]{6,15}|1[3-9][0-9]{9})$")
EMAIL_RE = re.compile(r"^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$")
DEPT_SEG_BAD = set('*?"|')
MAX_ITER = 8

# 部门名缩写表：修复「路径超长」时使用，优先从最深层往上缩，做到影响面最小。
DEPT_ABBR = {
    "华北交付组": "华北组",
    "实施交付部": "交付部",
    "交付与客户成功中心": "交付中心",
    "Web前端组": "Web组",
    "产品与设计中心": "产品中心",
}

# 职务里的「资深程度」关键词，用于「部门缺负责人」时挑一个最资深的顶上。
SENIORITY = ["首席执行官", "首席", "总经理", "总监", "负责人", "经理", "主管", "组长", "高级"]

# --------------------------------------------------------------------------
# 1. 组织架构原始数据（第 1 轮的「未经治理」状态）
#    m(姓名, 帐号, 职务, 性别, 邮箱=None 表示缺联系方式)
# --------------------------------------------------------------------------


def m(name: str, account: str, title: str, gender: str, email: str | None = "__auto__"):
    return {"name": name, "account": account, "title": title, "gender": gender, "email": email}


def d(name: str, leader: str | None, members: list[dict], children: list[dict] | None = None):
    return {"name": name, "leader": leader, "members": members, "children": children or []}


TREE = [
    d("总经办", "陈立群", [
        m("陈立群", "chenliqun", "总经理", "男"),
        m("周敏", "zhoumin", "总经理助理", "女"),
        m("赵国强", "zhaoguoqiang", "首席运营官", "男"),
        m("林静", "linjing", "首席财务官", "女"),
    ]),
    d("产品中心", "王涛", [
        m("王涛", "wangtao", "产品总监", "男"),
    ], [
        d("产品部", "李思远", [
            m("李思远", "lisiyuan", "高级产品经理", "男"),
            m("孙悦", "sunyue", "产品经理", "女"),
            m("吴桐", "wutong", "产品经理", "女"),
        ]),
        d("设计部", "郑雅", [
            m("郑雅", "zhengya", "设计主管", "女"),
            m("冯雪", "fengxue", "UI设计师", "女"),
            m("韩磊", "hanlei", "交互设计师", "男"),
        ]),
        # 种子：该部门没有指定负责人
        d("用户研究组", None, [
            m("徐腾", "xuteng", "用户研究员", "男"),
            m("何欣", "hexin", "用户研究员", "女"),
        ]),
    ]),
    d("技术中心", "刘建国", [
        m("刘建国", "liujianguo", "技术总监", "男"),
    ], [
        d("后端开发部", "曹阳", [
            m("曹阳", "caoyang", "后端开发经理", "男"),
        ], [
            # 种子：张伟 / 张威 拼音相同，朴素导出会撞帐号
            d("交易服务组", "张伟", [
                m("张伟", "zhangwei", "后端开发工程师", "男"),
                m("黄志强", "huangzhiqiang", "后端开发工程师", "男"),
                m("马超", "machao", "后端开发工程师", "男"),
            ]),
            d("用户服务组", "谢东", [
                m("谢东", "xiedong", "后端开发工程师", "男"),
                m("程亮", "chengliang", "后端开发工程师", "男"),
                m("张威", "zhangwei", "后端开发工程师", "男"),
            ]),
        ]),
        d("前端开发部", "吴倩", [
            m("吴倩", "wuqian", "前端开发经理", "女"),
        ], [
            d("Web前端组", "邓佳", [
                m("邓佳", "dengjia", "前端开发工程师", "女"),
                m("蒋文博", "jiangwenbo", "前端开发工程师", "男"),
            ]),
            d("移动端组", "罗鑫", [
                m("罗鑫", "luoxin", "移动端开发工程师", "男"),
                m("秦朗", "qinlang", "移动端开发工程师", "男"),
                m("潘晨", "panchen", "移动端开发工程师", "女"),
            ]),
        ]),
        d("算法与数据部", "唐磊", [
            m("唐磊", "tanglei", "算法负责人", "男"),
        ], [
            d("算法组", "沈亦", [
                m("沈亦", "shenyi", "算法工程师", "女"),
                m("杨帆", "yangfan", "算法工程师", "男"),
            ]),
            d("数据组", "段宇", [
                m("段宇", "duanyu", "数据工程师", "男"),
                m("汪清", "wangqing", "数据分析师", "女"),
            ]),
        ]),
        d("测试与质量部", "方婷", [
            # 种子：姓名尾随空格（CSV 导出常见）
            m("石磊 ", "shilei", "测试工程师", "男"),
            m("方婷", "fangting", "测试主管", "女"),
            m("白杨", "baiyang", "测试工程师", "男"),
        ]),
        d("运维与安全部", "顾明", [
            m("顾明", "guming", "运维负责人", "男"),
            m("崔浩", "cuihao", "SRE工程师", "男"),
            m("侯磊", "houlei", "安全工程师", "男"),
        ]),
    ]),
    d("市场中心", "苏航", [
        m("苏航", "suhang", "市场总监", "男"),
    ], [
        d("品牌市场部", "叶楠", [
            m("叶楠", "yenan", "品牌经理", "女"),
            # 种子：职务里带半角逗号
            m("周雨", "zhouyu", "内容运营,资深", "女"),
            m("卢佳", "lujia", "视觉设计师", "女"),
        ]),
        d("渠道增长部", "高鹏", [
            m("高鹏", "gaopeng", "增长负责人", "男"),
            m("许晨", "xuchen", "渠道运营", "男"),
            m("邹雪", "zouxue", "新媒体运营", "女"),
        ]),
    ]),
    d("销售中心", "沈国强", [
        m("沈国强", "shenguoqiang", "销售总监", "男"),
    ], [
        d("大客户部", "田伟", [
            m("田伟", "tianwei", "大客户经理", "男"),
            m("江楠", "jiangnan", "客户经理", "女"),
            m("卢鹏", "lupeng", "客户经理", "男"),
            m("常远", "changyuan", "售前顾问", "男"),
        ]),
        d("电商渠道部", "尹丽", [
            m("尹丽", "yinli", "电商渠道主管", "女"),
            m("陆洋", "luyang", "电商运营", "男"),
            m("温馨", "wenxin", "电商客服主管", "女"),
        ]),
    ]),
    d("交付与客户成功中心", "韩雪松", [
        m("韩雪松", "hanxuesong", "交付总监", "男"),
    ], [
        d("实施交付部", "韦东", [
            m("韦东", "weidong", "实施交付经理", "男"),
            m("岳鑫", "yuexin", "实施顾问", "男"),
            m("拾佳", "shijia", "实施顾问", "女"),
        ], [
            # 种子：四级路径 33 字 > 32 字上限
            d("华北交付组", "关鹏", [
                m("关鹏", "guanpeng", "交付工程师", "男"),
                # 种子：缺联系方式（手机与个人邮箱都为空）
                m("苗青", "miaoqing", "交付工程师", "女", None),
            ]),
        ]),
        d("客户成功部", "秦芳", [
            m("秦芳", "qinfang", "客户成功经理", "女"),
            m("廖凯", "liaokai", "客户成功专员", "男"),
            m("温故", "wengu", "技术支持工程师", "男"),
        ]),
    ]),
    d("职能中心", "蒋文", [
        m("蒋文", "jiangwen", "职能中心负责人", "女"),
    ], [
        d("人力资源部", "岳琳", [
            m("岳琳", "yuelin", "人力资源经理", "女"),
            m("谭雪", "tanxue", "招聘主管", "女"),
            # 种子：职务里带半角逗号
            m("毕晓", "bixiao", "HRBP,华东", "女"),
        ]),
        d("财务部", "章敏", [
            m("章敏", "zhangmin", "财务经理", "女"),
            m("万晴", "wanqing", "会计", "女"),
            m("严兵", "yanbing", "出纳", "男"),
        ]),
        # 种子：该部门没有指定负责人
        d("法务与合规部", None, [
            m("郝律", "haolv", "法务专员", "男"),
            m("任真", "renzhen", "合规专员", "女"),
        ]),
    ]),
]


# --------------------------------------------------------------------------
# 2. 构建与校验
# --------------------------------------------------------------------------


class Issue:
    """一条校验结果。level 为 error（必须修）或 warn（建议修）。"""

    def __init__(self, rule: str, level: str, where: str, detail: str, fix: str = ""):
        self.rule = rule
        self.level = level
        self.where = where
        self.detail = detail
        self.fix = fix

    def as_row(self) -> list[str]:
        return [self.level.upper(), self.rule, self.where, self.detail, self.fix]

    def __repr__(self) -> str:
        return f"[{self.level}] {self.rule} @ {self.where}: {self.detail}"


def iter_depts(tree: list[dict], prefix: list[str] | None = None):
    """深度优先遍历部门树，产出 (路径段列表, 部门节点, 父路径段列表)。"""
    prefix = list(prefix or [])
    for node in tree:
        segs = prefix + [node["name"]]
        yield segs, node, prefix
        yield from iter_depts(node["children"], segs)


def full_path(segs: list[str], drop_prefix: bool) -> str:
    parts = segs[1:] if drop_prefix else segs
    return "/".join(parts)


def build_rows(tree: list[dict], drop_prefix: bool = False) -> list[dict]:
    """把部门树拍平成待导入的行；同一部门内负责人排最前。"""
    rows: list[dict] = []
    for segs, node, _ in iter_depts(tree):
        path = full_path([COMPANY] + segs, drop_prefix)
        leader = node["leader"]
        ordered = sorted(
            node["members"], key=lambda x: (0 if x["name"].strip() == leader else 1)
        )
        for mb in ordered:
            email = mb["email"]
            if email == "__auto__":
                email = f"{mb['account'].strip()}@taskpilot-demo.cn"
            rows.append({
                "帐号": mb["account"],
                "姓名": mb["name"],
                "别名": "",
                "职务": mb["title"],
                "部门": path,
                "性别": mb["gender"],
                "手机": "",
                "座机": "",
                "个人邮箱": email or "",
                "_dept_segs": segs,
                "_is_leader": mb["name"].strip() == leader,
            })
    return rows


def validate(tree: list[dict], rows: list[dict], drop_prefix: bool = False) -> list[Issue]:
    """跑一遍全量规则校验，返回所有 issues。"""
    issues: list[Issue] = []

    # --- 部门层 ---
    dept_paths: dict[str, list[str]] = {}
    seen_full: set[str] = set()
    for segs, node, parent in iter_depts(tree):
        raw = node["name"]
        path = full_path([COMPANY] + segs, drop_prefix)
        dept_paths[path] = segs

        if raw != raw.strip():
            issues.append(Issue("D-01 部门名首尾空格", "error", path,
                                f"名称「{raw}」首尾含空白字符", "去除首尾空格"))
        if any(c in raw for c in DEPT_SEG_BAD):
            issues.append(Issue("D-02 部门名含非法字符", "error", path,
                                f"名称含 {sorted(set(raw) & DEPT_SEG_BAD)}", "移除 * ? \" |"))
        if len(path) > MAX_DEPT_FIELD_LEN:
            issues.append(Issue("D-03 部门字段超长", "error", path,
                                f"路径 {len(path)} 字符 > 上限 {MAX_DEPT_FIELD_LEN}", "缩写最深层部门名"))
        if "," in path or "，" in path:
            issues.append(Issue("D-04 部门路径含逗号", "error", path,
                                "字段值不允许出现逗号", "替换为顿号"))
        if len(segs) + (0 if drop_prefix else 1) > MAX_DEPT_DEPTH:
            issues.append(Issue("D-05 部门层级超限", "error", path,
                                f"层级 {len(segs) + 1} > 上限 {MAX_DEPT_DEPTH}", "压平层级"))
        if path in seen_full:
            issues.append(Issue("D-06 部门路径重复", "error", path, "同一条路径出现两次", "合并"))
        seen_full.add(path)

        if not node["members"]:
            issues.append(Issue("D-07 部门无直属成员", "warn", path,
                                "只作为中间节点存在，无直接成员", "可接受，或补充成员"))
        if not node["leader"]:
            issues.append(Issue("D-08 部门缺负责人", "warn", path,
                                "未指定部门负责人", "按职务资历自动指派"))

    # 同级部门重名
    for segs, node, parent in iter_depts(tree):
        siblings = [n["name"] for n in (_children_of(tree, parent) or [])]
        if siblings.count(node["name"]) > 1:
            issues.append(Issue("D-09 同级部门重名", "error", full_path([COMPANY] + segs, drop_prefix),
                                f"同级出现 {siblings.count(node['name'])} 个「{node['name']}」", "改名去重"))

    # --- 成员层 ---
    accounts: dict[str, int] = {}
    emails: dict[str, int] = {}
    members_total = 0
    for i, r in enumerate(rows, start=2):
        members_total += 1
        acct = r["帐号"]
        if not ACCOUNT_RE.match(acct or ""):
            issues.append(Issue("M-01 帐号格式非法", "error", f"第{i}行 {r['姓名']}",
                                f"「{acct}」不符合 1-32 位字母/数字/._-", "规范化帐号"))
        accounts[acct] = accounts.get(acct, 0) + 1

        nm = r["姓名"]
        if not nm or not nm.strip():
            issues.append(Issue("M-02 姓名必填", "error", f"第{i}行", "姓名为空", "补全姓名"))
        if nm != nm.strip():
            issues.append(Issue("M-03 姓名首尾空格", "error", f"第{i}行 {nm!r}",
                                "姓名含首尾空白", "trim"))
        for col in ("姓名", "职务", "部门", "别名", "个人邮箱", "手机", "座机"):
            v = r[col] or ""
            if "," in v or "，" in v:
                issues.append(Issue("M-04 字段含逗号", "error", f"第{i}行 {nm.strip()}",
                                    f"{col}=「{v}」含逗号", "替换为顿号"))
        if not r["部门"]:
            issues.append(Issue("M-05 部门必填", "error", f"第{i}行 {nm.strip()}", "部门为空", "指派部门"))
        if not (r["手机"] or r["个人邮箱"]):
            issues.append(Issue("M-06 缺联系方式", "error", f"第{i}行 {nm.strip()}",
                                "手机与个人邮箱同时为空（二选一必填）", "按帐号生成占位邮箱"))
        if r["手机"] and not MOBILE_RE.match(r["手机"]):
            issues.append(Issue("M-07 手机号格式非法", "error", f"第{i}行 {nm.strip()}",
                                f"「{r['手机']}」格式不正确", "修正手机号"))
        if r["个人邮箱"] and not EMAIL_RE.match(r["个人邮箱"]):
            issues.append(Issue("M-08 邮箱格式非法", "error", f"第{i}行 {nm.strip()}",
                                f"「{r['个人邮箱']}」格式不正确", "修正邮箱"))
        if r["性别"] not in ("男", "女", ""):
            issues.append(Issue("M-09 性别取值非法", "error", f"第{i}行 {nm.strip()}",
                                f"「{r['性别']}」", "改为 男/女"))
        if r["个人邮箱"]:
            emails[r["个人邮箱"]] = emails.get(r["个人邮箱"], 0) + 1

    for acct, n in accounts.items():
        if n > 1:
            issues.append(Issue("M-10 帐号重复", "error", f"帐号 {acct}",
                                f"出现 {n} 次（帐号在企业内唯一）", "追加数字后缀"))
    for mail, n in emails.items():
        if n > 1:
            issues.append(Issue("M-11 邮箱重复", "error", f"邮箱 {mail}",
                                f"出现 {n} 次（邮箱在企业内唯一）", "追加数字后缀"))
    if members_total > MAX_MEMBERS:
        issues.append(Issue("M-12 超出企业人数上限", "error", "全表",
                            f"{members_total} 人 > 未认证企业上限 {MAX_MEMBERS}", "精简名单"))
    return issues


def _children_of(tree: list[dict], prefix_segs: list[str]) -> list[dict] | None:
    """按路径段定位某部门的 children。"""
    nodes = tree
    for seg in prefix_segs:
        found = None
        for n in nodes:
            if n["name"] == seg:
                found = n
                break
        if found is None:
            return None
        nodes = found["children"]
    return nodes


# --------------------------------------------------------------------------
# 3. 修复
# --------------------------------------------------------------------------


def apply_fixes(tree: list[dict], rows: list[dict], issues: list[Issue], state: dict) -> list[str]:
    """按违规类别施加最小影响修复，返回本轮修复动作描述。"""
    log: list[str] = []

    # --- D-01 部门名首尾空格 / D-02 非法字符 / D-04 逗号 ---
    for segs, node, _ in iter_depts(tree):
        before = node["name"]
        after = before.strip()
        after = "".join(c for c in after if c not in DEPT_SEG_BAD)
        after = after.replace(",", "、").replace("，", "、")
        if after != before:
            node["name"] = after
            log.append(f"部门改名：{before!r} -> {after!r}")

    # --- M-03 姓名空格 / M-04 字段逗号 / M-08 邮箱补齐 ---
    for node in _all_nodes(tree):
        for mb in node["members"]:
            old = mb["name"]
            mb["name"] = old.strip()
            if mb["name"] != old:
                log.append(f"成员姓名去空格：{old!r} -> {mb['name']!r}")
            if "," in mb["title"] or "，" in mb["title"]:
                new_title = mb["title"].replace(",", "、").replace("，", "、")
                log.append(f"职务去逗号：{mb['title']!r} -> {new_title!r}")
                mb["title"] = new_title
            if mb["email"] is None:
                mb["email"] = "__auto__"
                log.append(f"补齐联系方式：{mb['name']} 按帐号 {mb['account']} 生成占位个人邮箱")

    # --- D-03 部门字段超长：从最深层往上缩，影响面最小；缩不动就全局去掉企业名前缀 ---
    if any(i.rule == "D-03 部门字段超长" for i in issues):
        for _ in range(8):  # 内部不动点迭代：每轮只动一处，动完重新测量
            over = [
                (segs, node)
                for segs, node, _ in iter_depts(tree)
                if len(full_path([COMPANY] + segs, state["drop_prefix"])) > MAX_DEPT_FIELD_LEN
            ]
            if not over:
                break
            handled = False
            segs, node = over[0]
            before_len = len(full_path([COMPANY] + segs, state["drop_prefix"]))
            for idx in range(len(segs) - 1, -1, -1):
                target = _node_at(tree, segs[: idx + 1])
                if target and target["name"] in DEPT_ABBR:
                    old = target["name"]
                    target["name"] = DEPT_ABBR[old]
                    after_len = len(full_path([COMPANY] + segs[:idx] + [target["name"]],
                                              state["drop_prefix"]))
                    affected = sum(1 for _ in _subtree(target))
                    log.append(
                        f"缩写部门名（从最深层往上）：{old} -> {DEPT_ABBR[old]}，"
                        f"影响该部门及其下级共 {affected} 个部门；"
                        f"被修复路径 {before_len} -> {after_len} 字符"
                    )
                    handled = True
                    break
            if not handled:
                if not state["drop_prefix"]:
                    state["drop_prefix"] = True
                    log.append(
                        f"缩写表已用尽仍超长（{before_len} 字符），全局回退为「不带企业名前缀」模式"
                        f"（每条路径减少 {len(COMPANY) + 1} 字符）"
                    )
                else:
                    log.append(f"无法自动修复的超长路径：{full_path([COMPANY] + segs, True)}")
                    break

    # --- D-08 部门缺负责人：按职务资历指派 ---
    for segs, node, _ in iter_depts(tree):
        if node["leader"]:
            continue
        if not node["members"]:
            continue
        pick = max(node["members"], key=lambda x: _seniority(x["title"]))
        node["leader"] = pick["name"]
        log.append(
            f"指派部门负责人：{full_path([COMPANY] + segs, state.get('drop_prefix', False))} "
            f"-> {pick['name']}（{pick['title']}）"
        )

    # --- M-10 帐号重复 / M-11 邮箱重复 ---
    seen: dict[str, int] = {}
    for node in _all_nodes(tree):
        for mb in node["members"]:
            acct = mb["account"]
            if acct in seen:
                seen[acct] += 1
                new = f"{acct}{seen[acct]}"
                log.append(f"帐号去重：{mb['name']} 的帐号 {acct} -> {new}（重名拼音冲突）")
                mb["account"] = new
            else:
                seen[acct] = 1
    mail_seen: dict[str, int] = {}
    for node in _all_nodes(tree):
        for mb in node["members"]:
            if mb["email"] == "__auto__":
                mb["email"] = f"{mb['account']}@taskpilot-demo.cn"
            mail = mb["email"] or ""
            if not mail:
                continue
            if mail in mail_seen:
                mail_seen[mail] += 1
                mb["email"] = f"{mb['account']}@taskpilot-demo.cn"
                log.append(f"邮箱去重：{mb['name']} -> {mb['email']}")
            else:
                mail_seen[mail] = 1

    return log


def _all_nodes(tree: list[dict]):
    for _, node, _ in iter_depts(tree):
        yield node


def _node_at(tree: list[dict], segs: list[str]) -> dict | None:
    nodes = tree
    node = None
    for seg in segs:
        node = None
        for n in nodes:
            if n["name"] == seg:
                node = n
                break
        if node is None:
            return None
        nodes = node["children"]
    return node


def _seniority(title: str) -> int:
    for rank, kw in enumerate(SENIORITY):
        if kw in title:
            return len(SENIORITY) - rank
    return 0


# --------------------------------------------------------------------------
# 4. 迭代循环
# --------------------------------------------------------------------------


def run_loop() -> tuple[list[dict], dict, list[dict]]:
    state = {"drop_prefix": False}
    tree = deepcopy(TREE)
    history: list[dict] = []

    for it in range(1, MAX_ITER + 1):
        rows = build_rows(tree, state["drop_prefix"])
        issues = validate(tree, rows, state["drop_prefix"])
        errs = [i for i in issues if i.level == "error"]
        warns = [i for i in issues if i.level == "warn"]
        score = max(0, 100 - 6 * len(errs) - 1 * len(warns))

        entry = {
            "iteration": it,
            "errors": errs,
            "warns": warns,
            "score": score,
            "fixes": [],
            "drop_prefix": state["drop_prefix"],
        }

        if not errs:
            history.append(entry)
            print(f"[第 {it} 轮] 错误 0 / 警告 {len(warns)} / 得分 {score}  -> 收敛，停止迭代")
            break

        fixes = apply_fixes(tree, rows, issues, state)
        entry["fixes"] = fixes
        history.append(entry)
        print(f"[第 {it} 轮] 错误 {len(errs)} / 警告 {len(warns)} / 得分 {score}  "
              f"-> 施加 {len(fixes)} 项修复")
        for f in fixes:
            print(f"          · {f}")

    rows = build_rows(tree, state["drop_prefix"])
    final_issues = validate(tree, rows, state["drop_prefix"])
    return rows, {"tree": tree, "state": state, "final_issues": final_issues}, history


# --------------------------------------------------------------------------
# 5. 输出 Excel
# --------------------------------------------------------------------------


THIN = Side(style="thin", color="D0D5DD")
BORDER = Border(left=THIN, right=THIN, top=THIN, bottom=THIN)
HDR_FILL = PatternFill("solid", fgColor="1F4E79")
HDR_FONT = Font(bold=True, color="FFFFFF", size=11)
REQ_FONT = Font(bold=True, color="C00000", size=11)
BODY_FONT = Font(size=11)
LEADER_FONT = Font(size=11, bold=True, color="1F4E79")


def style_sheet(ws, headers: list[str], widths: list[int], required: set[str] | None = None):
    required = required or set()
    for c, (h, w) in enumerate(zip(headers, widths), start=1):
        cell = ws.cell(row=1, column=c, value=h)
        cell.fill = HDR_FILL
        cell.font = REQ_FONT if h in required else HDR_FONT
        cell.alignment = Alignment(horizontal="center", vertical="center")
        cell.border = BORDER
        ws.column_dimensions[get_column_letter(c)].width = w
    ws.row_dimensions[1].height = 24
    ws.freeze_panes = "A2"


def write_import_workbook(rows: list[dict], path: Path):
    wb = Workbook()
    ws = wb.active
    ws.title = "成员信息"
    style_sheet(ws, HEADERS, [14, 10, 10, 20, 34, 6, 14, 14, 30], REQUIRED_HEADERS)
    for i, r in enumerate(rows, start=2):
        for c, h in enumerate(HEADERS, start=1):
            cell = ws.cell(row=i, column=c, value=r[h])
            cell.font = LEADER_FONT if (h == "姓名" and r.get("_is_leader")) else BODY_FONT
            cell.border = BORDER
            cell.alignment = Alignment(vertical="center",
                                       horizontal="center" if h in ("性别", "手机", "座机") else "left")
    ws.auto_filter.ref = f"A1:{get_column_letter(len(HEADERS))}{len(rows) + 1}"
    wb.save(path)


def write_reference_workbook(rows, result, history, out_path: Path):
    tree = result["tree"]
    drop_prefix = result["state"]["drop_prefix"]
    final_issues = result["final_issues"]
    wb = Workbook()

    # --- 组织架构总览 ---
    ws = wb.active
    ws.title = "组织架构总览"
    style_sheet(ws, ["层级", "部门", "部门路径（导入字段值）", "负责人", "直属人数", "含下级人数", "路径字符数"],
                [8, 24, 46, 12, 10, 12, 12])
    r = 2
    for segs, node, _ in iter_depts(tree):
        dept_path = full_path([COMPANY] + segs, drop_prefix)
        sub = sum(len(n["members"]) for n in _subtree(node))
        ws.cell(row=r, column=1, value=len(segs) + (0 if drop_prefix else 1)).alignment = Alignment(horizontal="center")
        ws.cell(row=r, column=2, value=node["name"]).font = Font(bold=(len(segs) == 0), size=11)
        ws.cell(row=r, column=3, value=dept_path)
        ws.cell(row=r, column=4, value=node["leader"] or "—")
        ws.cell(row=r, column=5, value=len(node["members"])).alignment = Alignment(horizontal="center")
        ws.cell(row=r, column=6, value=sub).alignment = Alignment(horizontal="center")
        ws.cell(row=r, column=7, value=len(dept_path)).alignment = Alignment(horizontal="center")
        for c in range(1, 8):
            ws.cell(row=r, column=c).border = BORDER
        r += 1

    # --- 成员花名册 ---
    ws2 = wb.create_sheet("成员花名册")
    cols2 = ["序号", "姓名", "帐号", "职务", "部门", "性别", "主部门", "是否部门负责人", "个人邮箱"]
    style_sheet(ws2, cols2, [6, 10, 18, 20, 34, 6, 34, 14, 30])
    for i, row in enumerate(rows, start=1):
        vals = [i, row["姓名"], row["帐号"], row["职务"], row["部门"], row["性别"],
                row["部门"].split(";")[0], "是" if row.get("_is_leader") else "", row["个人邮箱"]]
        for c, v in enumerate(vals, start=1):
            cell = ws2.cell(row=i + 1, column=c, value=v)
            cell.font = BODY_FONT
            cell.border = BORDER

    # --- 部门负责人 ---
    ws3 = wb.create_sheet("部门负责人")
    style_sheet(ws3, ["部门路径", "部门名称", "负责人", "负责人职务", "设置方式"],
                [46, 22, 12, 22, 34])
    rr = 2
    for segs, node, _ in iter_depts(tree):
        dept_path = full_path([COMPANY] + segs, drop_prefix)
        title = ""
        for mb in node["members"]:
            if mb["name"] == node["leader"]:
                title = mb["title"]
        vals = [dept_path, node["name"], node["leader"] or "—", title,
                "文件导入不含该字段，导入后在后台勾选，或用 user/create 的 is_leader_in_dept"]
        for c, v in enumerate(vals, start=1):
            cell = ws3.cell(row=rr, column=c, value=v)
            cell.font = BODY_FONT
            cell.border = BORDER
        rr += 1

    # --- 校验报告 ---
    ws4 = wb.create_sheet("校验报告")
    style_sheet(ws4, ["级别", "规则", "位置", "说明", "建议修复"], [8, 22, 34, 46, 30])
    rr = 2
    if final_issues:
        for iss in final_issues:
            for c, v in enumerate(iss.as_row(), start=1):
                cell = ws4.cell(row=rr, column=c, value=v)
                cell.font = Font(size=10, color="C00000" if iss.level == "error" else "9A6700")
                cell.border = BORDER
            rr += 1
    else:
        cell = ws4.cell(row=rr, column=1, value="—")
        cell.border = BORDER
        ws4.cell(row=rr, column=2, value="全部通过").font = Font(bold=True, color="1A7F37")

    # --- 迭代日志 ---
    ws5 = wb.create_sheet("迭代日志")
    style_sheet(ws5, ["轮次", "错误数", "警告数", "合规得分", "企业名前缀", "本轮修复动作"],
                [6, 8, 8, 10, 12, 96])
    rr = 2
    for h in history:
        ws5.cell(row=rr, column=1, value=h["iteration"]).alignment = Alignment(horizontal="center")
        ws5.cell(row=rr, column=2, value=len(h["errors"])).alignment = Alignment(horizontal="center")
        ws5.cell(row=rr, column=3, value=len(h["warns"])).alignment = Alignment(horizontal="center")
        ws5.cell(row=rr, column=4, value=h["score"]).alignment = Alignment(horizontal="center")
        ws5.cell(row=rr, column=5, value="带" if not h["drop_prefix"] else "不带").alignment = Alignment(horizontal="center")
        ws5.cell(row=rr, column=6, value="\n".join(h["fixes"]) if h["fixes"] else "（无，已收敛）")
        for c in range(1, 7):
            ws5.cell(row=rr, column=c).border = BORDER
            ws5.cell(row=rr, column=c).alignment = Alignment(
                wrap_text=(c == 6), vertical="top",
                horizontal="center" if c != 6 else "left")
        ws5.row_dimensions[rr].height = max(18, 15 * max(1, len(h["fixes"])))
        rr += 1

    # --- 导入说明 ---
    ws6 = wb.create_sheet("导入说明")
    ws6.column_dimensions["A"].width = 8
    ws6.column_dimensions["B"].width = 118
    lines = [
        ("一、怎么导入", [
            "1. 用管理员账号登录 https://work.weixin.qq.com/ ，左侧进入【通讯录】。",
            "2. 右上角【批量导入/导出】-> 选择【文件导入】-> 上传本目录下的",
            "   《企业微信通讯录导入-成员信息.xlsx》（只上传这一个文件，本说明文件不要传）。",
            "3. 上传后系统会自动建出表里出现过的全部部门，并弹出预览让您核对姓名 / 帐号 / 部门；",
            "   确认无误再点【导入】。帐号、手机号、邮箱在企业内唯一，帐号相同会覆盖导入，可放心重导。",
            "4. 建议第一次先只留 3 行试导一次，确认列名与部门层级都对，再把整表导进去。",
        ]),
        ("二、本表的字段口径（依据官方帮助文档 2026-09 核对）", [
            "· 必填：姓名、帐号、部门，以及手机与个人邮箱二选一 —— 表头用红色标出。",
            "· 帐号：1~32 位，只允许字母、数字、点、减号、下划线；企业内唯一。",
            "· 部门：上下级用「/」分隔，从最上级部门开始；本表已用企业名 taskpilot怀民 作为第一段，",
            "  与官方模板示例（腾讯公司/微信事业群/广州研发部）的写法一致。多部门用「;」分隔，第一个为主部门。",
            "· 部门字段（整条路径）长度不得超过 32 个字符 —— 本表已逐条校验通过。",
            "· 字段值里不能出现半角逗号「,」—— 本表已逐条校验通过。",
            "· 手机号留空是刻意的：企业微信会校验手机号是否已被占用，填假号极易整表导入失败。",
            "  这些成员不影响您调通讯录 / 组织架构接口（未激活成员同样会被 user/list 返回）。",
            "  若之后要让成员能真的激活登录，把「个人邮箱」换成同事本人的真实手机号即可。",
        ]),
        ("三、如果管理后台提示列名不识别", [
            "本表列名取自官方帮助文档的字段罗列。若您的后台模板列名有差异（例如「帐号」写作「账号」），",
            "只需把官方模板的首行覆盖本表首行，数据行保持不动即可。",
        ]),
        ("四、如果导入后发现多出一层部门", [
            "说明企业微信的根部门名称与本表前缀 taskpilot怀民 不完全一致。把「部门」列里的",
            "「taskpilot怀民/」批量替换为空，再按同样方式重导一次即可（帐号相同会覆盖，不会产生重复成员）。",
        ]),
        ("五、调通讯录 / 组织架构接口", [
            "· 拉部门：GET https://qyapi.weixin.qq.com/cgi-bin/department/list?access_token=TOKEN",
            "· 拉成员：GET https://qyapi.weixin.qq.com/cgi-bin/user/list?access_token=TOKEN"
            "&department_id=1&fetch_child=1",
            "· 拉单人：GET https://qyapi.weixin.qq.com/cgi-bin/user/get?access_token=TOKEN&userid=chenliqun",
            "· 本表可直接用来验证的接口字段：department（部门 id 数组）、is_leader_in_dept、position、gender、status。",
            "· 「部门负责人」不在文件导入的字段里，导入后可在后台部门详情里勾选，",
            "  或调用 user/create 时传 is_leader_in_dept 设置 —— 详见「部门负责人」工作表。",
        ]),
    ]
    rr = 1
    for title, body in lines:
        c = ws6.cell(row=rr, column=1, value=title)
        c.font = Font(bold=True, size=12, color="1F4E79")
        rr += 1
        for line in body:
            cell = ws6.cell(row=rr, column=2, value=line)
            cell.font = BODY_FONT
            cell.alignment = Alignment(wrap_text=False, vertical="center")
            rr += 1
        rr += 1

    wb.save(out_path)


def _subtree(node: dict):
    yield node
    for ch in node["children"]:
        yield from _subtree(ch)


# --------------------------------------------------------------------------
# 6. main
# --------------------------------------------------------------------------


def main():
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    print("=" * 78)
    print("企业微信通讯录导入表 · 生成循环")
    print("=" * 78)

    rows, result, history = run_loop()
    tree = result["tree"]
    final_issues = result["final_issues"]

    n_dept = sum(1 for _ in iter_depts(tree))
    n_member = len(rows)
    max_len = max(len(r["部门"]) for r in rows)
    max_depth = max(len(s) + (0 if result["state"]["drop_prefix"] else 1)
                    for s, _, _ in iter_depts(tree))

    print("-" * 78)
    print(f"部门 {n_dept} 个 / 成员 {n_member} 人 / 最大层级 {max_depth} 层 / "
          f"最长部门路径 {max_len} 字符（上限 {MAX_DEPT_FIELD_LEN}）")
    print(f"最终校验：错误 {sum(1 for i in final_issues if i.level == 'error')} 条 / "
          f"警告 {sum(1 for i in final_issues if i.level == 'warn')} 条")
    print(f"企业名前缀模式：{'带' if not result['state']['drop_prefix'] else '不带'}")

    import_path = OUT_DIR / "企业微信通讯录导入-成员信息.xlsx"
    ref_path = OUT_DIR / "企业微信组织架构与导入说明.xlsx"
    write_import_workbook(rows, import_path)
    write_reference_workbook(rows, result, history, ref_path)
    print("-" * 78)
    print(f"已生成：{import_path}")
    print(f"已生成：{ref_path}")
    print(f"生成时间：{datetime.now():%Y-%m-%d %H:%M:%S}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
