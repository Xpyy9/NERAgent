// src/shims-vue.d.ts

declare module '*.vue' {
    import { DefineComponent } from 'vue'
    // 声明所有的 .vue 文件都导出一个 Vue 组件类型
    const component: DefineComponent<{}, {}, any>
    export default component
}