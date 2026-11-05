; ModuleID = '/tmp/blackburn-shader-0.bc'
source_filename = "imp_advanced_blend_vertex_main"
target datalayout = "e-p:64:64:64-i1:8:8-i8:8:8-i16:16:16-i32:32:32-i64:64:64-f32:32:32-f64:64:64-v16:16:16-v24:32:32-v32:32:32-v48:64:64-v64:64:64-v96:128:128-v128:128:128-v192:256:256-v256:256:256-v512:512:512-v1024:1024:1024-n8:16:32"
target triple = "air64-apple-ios13.0.0-simulator"

%struct.FrameInfo = type { %"struct.metal::matrix" }
%"struct.metal::matrix" = type { [4 x <4 x float>] }

@llvm.global_ctors = appending global [0 x { i32, ptr, ptr }] zeroinitializer

; Function Attrs: minsize norecurse nounwind optsize memory(none)
define <{ <2 x float>, <2 x float>, <4 x float> }> @imp_advanced_blend_vertex_main(<2 x float> %0, <2 x float> %1, <2 x float> %2, ptr addrspace(2) readonly captures(none) dereferenceable(64) "air-buffer-no-alias" %3) local_unnamed_addr #0 {
  %5 = getelementptr inbounds %struct.FrameInfo, ptr addrspace(2) %3, i64 0, i32 0, i32 0, i64 0
  %6 = load <4 x float>, ptr addrspace(2) %5, align 16, !tbaa !21, !alias.scope !24
  %7 = shufflevector <2 x float> %0, <2 x float> undef, <4 x i32> zeroinitializer
  %8 = fmul fast <4 x float> %6, %7
  %9 = getelementptr inbounds %struct.FrameInfo, ptr addrspace(2) %3, i64 0, i32 0, i32 0, i64 1
  %10 = load <4 x float>, ptr addrspace(2) %9, align 16, !tbaa !21, !alias.scope !24
  %11 = shufflevector <2 x float> %0, <2 x float> undef, <4 x i32> <i32 1, i32 1, i32 1, i32 1>
  %12 = fmul fast <4 x float> %10, %11
  %13 = fadd fast <4 x float> %12, %8
  %14 = getelementptr inbounds %struct.FrameInfo, ptr addrspace(2) %3, i64 0, i32 0, i32 0, i64 3
  %15 = load <4 x float>, ptr addrspace(2) %14, align 16, !tbaa !21, !alias.scope !24
  %16 = fadd fast <4 x float> %13, %15
  %17 = insertvalue <{ <2 x float>, <2 x float>, <4 x float> }> undef, <2 x float> %1, 0
  %18 = insertvalue <{ <2 x float>, <2 x float>, <4 x float> }> %17, <2 x float> %2, 1
  %19 = insertvalue <{ <2 x float>, <2 x float>, <4 x float> }> %18, <4 x float> %16, 2
  ret <{ <2 x float>, <2 x float>, <4 x float> }> %19
}

attributes #0 = { minsize norecurse nounwind optsize memory(none) "approx-func-fp-math"="true" "frame-pointer"="all" "min-legal-vector-width"="128" "no-builtins" "no-infs-fp-math"="true" "no-nans-fp-math"="true" "no-signed-zeros-fp-math"="true" "no-trapping-math"="true" "stack-protector-buffer-size"="8" "unsafe-fp-math"="true" }

!llvm.module.flags = !{!0, !1, !2, !3}
!llvm.ident = !{!4}
!air.version = !{!5}
!air.language_version = !{!6}
!air.compile_options = !{!7, !8, !9}
!air.vertex = !{!10}

!0 = !{i32 2, !"SDK Version", [2 x i32] [i32 26, i32 2]}
!1 = !{i32 7, !"Dwarf Version", i32 4}
!2 = !{i32 2, !"Debug Info Version", i32 3}
!3 = !{i32 1, !"wchar_size", i32 4}
!4 = !{!"Apple metal version 32023.864 (metalfe-32023.864)"}
!5 = !{i32 2, i32 2, i32 0}
!6 = !{!"Metal", i32 1, i32 2, i32 0}
!7 = !{!"air.compile.denorms_disable"}
!8 = !{!"air.compile.fast_math_enable"}
!9 = !{!"air.compile.framebuffer_fetch_enable"}
!10 = !{ptr @imp_advanced_blend_vertex_main, !11, !15}
!11 = !{!12, !13, !14}
!12 = !{!"air.vertex_output", !"user(locn0)", !"air.arg_type_name", !"float2", !"air.arg_name", !"v_dst_texture_coords"}
!13 = !{!"air.vertex_output", !"user(locn1)", !"air.arg_type_name", !"float2", !"air.arg_name", !"v_src_texture_coords"}
!14 = !{!"air.position", !"air.arg_type_name", !"float4", !"air.arg_name", !"gl_Position"}
!15 = !{!16, !17, !18, !19}
!16 = !{i32 0, !"air.vertex_input", !"air.location_index", i32 0, i32 1, !"generated(__air_placeholder__)", !"air.arg_type_name", !"float2", !"air.arg_name", !"vertices"}
!17 = !{i32 1, !"air.vertex_input", !"air.location_index", i32 1, i32 1, !"generated(__air_placeholder__)", !"air.arg_type_name", !"float2", !"air.arg_name", !"dst_texture_coords"}
!18 = !{i32 2, !"air.vertex_input", !"air.location_index", i32 2, i32 1, !"generated(__air_placeholder__)", !"air.arg_type_name", !"float2", !"air.arg_name", !"src_texture_coords"}
!19 = !{i32 3, !"air.buffer", !"air.buffer_size", i32 64, !"air.location_index", i32 0, i32 1, !"air.read", !"air.struct_type_info", !20, !"air.arg_type_size", i32 64, !"air.arg_type_align_size", i32 16, !"air.arg_type_name", !"FrameInfo", !"air.arg_name", !"frame_info"}
!20 = !{i32 0, i32 64, i32 0, !"float4x4", !"mvp"}
!21 = !{!22, !22, i64 0}
!22 = !{!"omnipotent char", !23, i64 0}
!23 = !{!"Simple C++ TBAA"}
!24 = !{!25}
!25 = distinct !{!25, !26, !"air-alias-scope-arg(3)"}
!26 = distinct !{!26, !"air-alias-scopes(imp_advanced_blend_vertex_main)"}
